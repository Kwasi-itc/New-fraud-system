package eventstore

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const clickHouseTimestampLayout = "2006-01-02 15:04:05.000"

type clickHouseClient struct {
	baseURL              string
	database             string
	user                 string
	password             string
	client               *http.Client
	schemas              *sync.Map
	factSources          *sync.Map
	factShapes           *sync.Map
	factInitMu           *sync.Mutex
	factsEnabled         bool
	factMaxKeys          int
	factMaxKeysPerTenant int
	serverQueryTimeout   time.Duration
}

func newClickHouseClient(cfg Config) clickHouseClient {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if cfg.MaxConns > 0 {
		transport.MaxConnsPerHost = cfg.MaxConns
	}
	if cfg.MaxIdleConns > 0 {
		transport.MaxIdleConns = cfg.MaxIdleConns
		transport.MaxIdleConnsPerHost = cfg.MaxIdleConns
	}
	if cfg.IdleConnTimeout > 0 {
		transport.IdleConnTimeout = cfg.IdleConnTimeout
	}
	serverQueryTimeout := cfg.HTTPTimeout
	if serverQueryTimeout > time.Second {
		serverQueryTimeout -= time.Second
	}
	return clickHouseClient{
		baseURL:              cfg.ClickHouseURL,
		database:             cfg.ClickHouseDatabase,
		user:                 cfg.ClickHouseUser,
		password:             cfg.ClickHousePassword,
		client:               &http.Client{Timeout: cfg.HTTPTimeout, Transport: transport},
		schemas:              &sync.Map{},
		factSources:          &sync.Map{},
		factShapes:           &sync.Map{},
		factInitMu:           &sync.Mutex{},
		factsEnabled:         !cfg.DisableAggregateFacts,
		factMaxKeys:          maxInt(1, cfg.FeatureMaxKeys),
		factMaxKeysPerTenant: maxInt(1, cfg.FeatureMaxKeysPerTenant),
		serverQueryTimeout:   serverQueryTimeout,
	}
}

func (c clickHouseClient) close() {
	if transport, ok := c.client.Transport.(interface{ CloseIdleConnections() }); ok {
		transport.CloseIdleConnections()
	}
}

func (c clickHouseClient) initialize(ctx context.Context) error {
	if !identifierPattern.MatchString(c.database) {
		return fmt.Errorf("invalid ClickHouse database name")
	}
	if _, err := c.execute(ctx, "CREATE DATABASE IF NOT EXISTS "+c.database, nil); err != nil {
		return err
	}
	if c.factsEnabled {
		if err := c.initializeAggregateFactTables(ctx); err != nil {
			return err
		}
	}
	return c.rejectLegacyJSONRows(ctx)
}

func (c clickHouseClient) rejectLegacyJSONRows(ctx context.Context) error {
	body, err := c.execute(ctx, "SELECT count() AS value FROM system.tables WHERE database = "+quote(c.database)+" AND name = 'event_records' FORMAT JSONEachRow", nil)
	if err != nil {
		return err
	}
	var tableCount struct {
		Value uint64 `json:"value"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(body), &tableCount); err != nil {
		return fmt.Errorf("inspect legacy event table: %w", err)
	}
	if tableCount.Value == 0 {
		return nil
	}
	body, err = c.execute(ctx, "SELECT count() AS value FROM "+c.database+".event_records FORMAT JSONEachRow", nil)
	if err != nil {
		return err
	}
	var rowCount struct {
		Value uint64 `json:"value"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(body), &rowCount); err != nil {
		return fmt.Errorf("count legacy event rows: %w", err)
	}
	if rowCount.Value > 0 {
		return fmt.Errorf("legacy ClickHouse event_records contains %d JSON rows; typed event storage will not ignore them: migrate the data or start with a fresh ClickHouse volume", rowCount.Value)
	}
	return nil
}

func (c clickHouseClient) health(ctx context.Context) error {
	_, err := c.execute(ctx, "SELECT 1", nil)
	return err
}

func (c clickHouseClient) insert(ctx context.Context, table TableContract, events []Event) error {
	if err := c.ensureTable(ctx, table); err != nil {
		return err
	}

	var body bytes.Buffer
	encoder := json.NewEncoder(&body)
	for _, event := range events {
		row, err := buildInsertRow(table, event)
		if err != nil {
			return err
		}
		if err := encoder.Encode(row); err != nil {
			return err
		}
	}

	query := "INSERT INTO " + c.tableIdentifier(table) + " SETTINGS async_insert=1, wait_for_async_insert=1, async_insert_busy_timeout_ms=25 FORMAT JSONEachRow"
	_, err := c.execute(ctx, query, &body)
	return err
}

func (c clickHouseClient) ensureTable(ctx context.Context, table TableContract) error {
	if err := table.validate(); err != nil {
		return err
	}
	cacheKey := c.tableIdentifier(table)
	if cached, ok := c.schemas.Load(cacheKey); ok && cached == table.SchemaRevision {
		return nil
	}

	fieldNames := sortedFieldNames(table.Fields)
	columns := []string{
		"`_event_id` UUID",
		"`_revision_id` String",
		"`_schema_revision` String",
		"`_ingested_at` DateTime64(3, 'UTC')",
		"`_request_hash` String",
	}
	for _, name := range fieldNames {
		dataType, err := clickHouseType(table.Fields[name])
		if err != nil {
			return fmt.Errorf("field %s: %w", name, err)
		}
		columns = append(columns, quoteIdentifier(name)+" "+dataType)
	}
	columns = append(columns, "INDEX event_object_id_bloom `object_id` TYPE bloom_filter(0.01) GRANULARITY 4")
	for _, name := range fieldNames {
		if !table.Fields[name].IsProjection {
			continue
		}
		columns = append(columns,
			"PROJECTION "+quoteIdentifier(eventProjectionName(name))+" (SELECT * ORDER BY ("+
				quoteIdentifier(name)+", "+quoteIdentifier(table.EventTimeField)+", `object_id`, `_event_id`))",
		)
	}

	query := "CREATE TABLE IF NOT EXISTS " + c.tableIdentifier(table) + " (\n" +
		strings.Join(columns, ",\n") + "\n) ENGINE = ReplacingMergeTree(`_ingested_at`)\n" +
		"PARTITION BY toYYYYMM(" + quoteIdentifier(table.EventTimeField) + ")\n" +
		"ORDER BY (" + quoteIdentifier(table.EventTimeField) + ", `object_id`, `_event_id`)\n" +
		"SETTINGS index_granularity = 8192, deduplicate_merge_projection_mode = 'rebuild'"
	if _, err := c.execute(ctx, query, nil); err != nil {
		return err
	}
	if c.factsEnabled {
		if err := c.ensureFactGenerationView(ctx, table); err != nil {
			return err
		}
	} else if err := c.dropFactGenerationView(ctx, table); err != nil {
		return err
	}
	if err := c.verifyTableSchema(ctx, table); err != nil {
		return err
	}
	c.schemas.Store(cacheKey, table.SchemaRevision)
	return nil
}

func eventProjectionName(fieldName string) string {
	return "event_by_" + fieldName
}

func (c clickHouseClient) verifyTableSchema(ctx context.Context, table TableContract) error {
	body, err := c.execute(ctx, "DESCRIBE TABLE "+c.tableIdentifier(table)+" FORMAT JSONEachRow", nil)
	if err != nil {
		return err
	}
	expected := map[string]string{
		"_event_id":        "UUID",
		"_revision_id":     "String",
		"_schema_revision": "String",
		"_ingested_at":     "DateTime64(3,'UTC')",
		"_request_hash":    "String",
	}
	for name, field := range table.Fields {
		dataType, err := clickHouseType(field)
		if err != nil {
			return err
		}
		expected[name] = normalizeClickHouseType(dataType)
	}
	actual := make(map[string]string, len(expected))
	scanner := bufio.NewScanner(bytes.NewReader(body))
	for scanner.Scan() {
		var row struct {
			Name string `json:"name"`
			Type string `json:"type"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
			return fmt.Errorf("decode ClickHouse table schema: %w", err)
		}
		actual[row.Name] = normalizeClickHouseType(row.Type)
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if len(actual) != len(expected) {
		return fmt.Errorf("event table schema is immutable: ClickHouse table has %d columns but data model requires %d", len(actual), len(expected))
	}
	for name, expectedType := range expected {
		if actual[name] != expectedType {
			return fmt.Errorf("event table schema is immutable: column %s is %s but data model requires %s", name, actual[name], expectedType)
		}
	}
	if err := c.verifyTableProjections(ctx, table); err != nil {
		return err
	}
	return nil
}

func (c clickHouseClient) verifyTableProjections(ctx context.Context, table TableContract) error {
	body, err := c.execute(ctx,
		"SELECT name FROM system.projections WHERE database = "+quote(c.database)+
			" AND table = "+quote(physicalTableName(table.TenantID, table.TableID))+" FORMAT JSONEachRow", nil)
	if err != nil {
		return err
	}
	expected := make(map[string]struct{})
	for name, field := range table.Fields {
		if field.IsProjection {
			expected[eventProjectionName(name)] = struct{}{}
		}
	}
	actual := make(map[string]struct{})
	scanner := bufio.NewScanner(bytes.NewReader(body))
	for scanner.Scan() {
		var row struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
			return fmt.Errorf("decode ClickHouse projections: %w", err)
		}
		actual[row.Name] = struct{}{}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if len(actual) != len(expected) {
		return fmt.Errorf("event table schema is immutable: ClickHouse table has %d projections but data model requires %d", len(actual), len(expected))
	}
	for name := range expected {
		if _, ok := actual[name]; !ok {
			return fmt.Errorf("event table schema is immutable: ClickHouse projection %s is missing", name)
		}
	}
	return nil
}

func buildInsertRow(table TableContract, event Event) (map[string]any, error) {
	row := map[string]any{
		"_event_id":        event.EventID,
		"_revision_id":     table.RevisionID,
		"_schema_revision": table.SchemaRevision,
		"_ingested_at":     event.IngestedAt.UTC().Format(clickHouseTimestampLayout),
		"_request_hash":    event.RequestHash,
	}
	for name, field := range table.Fields {
		value, exists := event.Payload[name]
		switch name {
		case "object_id":
			value, exists = event.ObjectID, true
		case table.EventTimeField:
			value, exists = event.EventTime, true
		case "updated_at":
			value, exists = event.IngestedAt, true
		}
		if !exists {
			if field.Nullable {
				row[name] = nil
				continue
			}
			return nil, fmt.Errorf("non-null field %s is missing", name)
		}
		normalized, err := clickHouseValue(value, field)
		if err != nil {
			return nil, fmt.Errorf("field %s: %w", name, err)
		}
		row[name] = normalized
	}
	return row, nil
}

func clickHouseValue(value any, field FieldContract) (any, error) {
	if value == nil {
		if field.Nullable {
			return nil, nil
		}
		return nil, fmt.Errorf("null is not allowed")
	}
	if field.DataType != "timestamp" {
		return value, nil
	}
	switch typed := value.(type) {
	case time.Time:
		return typed.UTC().Format(clickHouseTimestampLayout), nil
	case string:
		parsed, err := time.Parse(time.RFC3339Nano, typed)
		if err != nil {
			return nil, fmt.Errorf("invalid timestamp: %w", err)
		}
		return parsed.UTC().Format(clickHouseTimestampLayout), nil
	default:
		return nil, fmt.Errorf("timestamp must be RFC3339 text")
	}
}

func (c clickHouseClient) getRecord(ctx context.Context, request RecordRequest) (map[string]any, error) {
	if err := request.Table.validate(); err != nil {
		return nil, err
	}
	query := "SELECT " + selectFieldList(request.Table) + " FROM " + c.tableIdentifier(request.Table) +
		" WHERE `object_id` = " + quote(request.ObjectID) + " ORDER BY `_ingested_at` DESC LIMIT 1 FORMAT JSONEachRow"
	body, err := c.execute(ctx, query, nil)
	if err != nil {
		return nil, err
	}
	rows, err := decodeRecordRows(body)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("record not found")
	}
	return rows[0], nil
}

func (c clickHouseClient) listRecords(ctx context.Context, request RecordRequest) ([]map[string]any, error) {
	if err := request.Table.validate(); err != nil {
		return nil, err
	}
	if request.Limit <= 0 || request.Limit > 1000 {
		request.Limit = 100
	}
	where := "1 = 1"
	if request.Field != "" {
		field, ok := request.Table.Fields[request.Field]
		if !ok {
			return nil, fmt.Errorf("field is not in the data model")
		}
		literal, err := typedStringLiteral(request.Value, field.DataType)
		if err != nil {
			return nil, err
		}
		where += " AND " + quoteIdentifier(request.Field) + " = " + literal
	}
	query := "SELECT " + selectFieldList(request.Table) + " FROM " + c.tableIdentifier(request.Table) + " WHERE " + where +
		" ORDER BY " + quoteIdentifier(request.Table.EventTimeField) + " DESC LIMIT " + strconv.Itoa(request.Limit) + " FORMAT JSONEachRow"
	body, err := c.execute(ctx, query, nil)
	if err != nil {
		return nil, err
	}
	return decodeRecordRows(body)
}

func (c clickHouseClient) aggregate(ctx context.Context, request AggregateRequest) (any, error) {
	fieldExpr, err := fieldExpression(request.Field, request.Table.Fields)
	if err != nil {
		return nil, err
	}
	aggregateExpr, err := aggregateExpression(request.Aggregate, fieldExpr)
	if err != nil {
		return nil, err
	}
	where := "1 = 1"
	if request.Filter != nil {
		filterSQL, err := buildFilterSQL(*request.Filter, request.Table.Fields)
		if err != nil {
			return nil, err
		}
		if filterSQL != "" {
			where += " AND (" + filterSQL + ")"
		}
	}
	query := "SELECT " + aggregateExpr + " AS value FROM " + c.tableIdentifier(request.Table) + " WHERE " + where + " FORMAT JSONEachRow"
	body, err := c.execute(ctx, query, nil)
	if err != nil {
		return nil, err
	}
	var row struct {
		Value any `json:"value"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(body), &row); err != nil {
		return nil, fmt.Errorf("decode aggregate response: %w", err)
	}
	return row.Value, nil
}

func (c clickHouseClient) aggregateBatch(ctx context.Context, requests []AggregateRequest) ([]any, error) {
	selects := make([]string, len(requests))
	filters := make([]string, len(requests))
	for i, request := range requests {
		fieldExpr, err := fieldExpression(request.Field, request.Table.Fields)
		if err != nil {
			return nil, err
		}
		filterSQL, err := buildFilterSQL(*request.Filter, request.Table.Fields)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(filterSQL) == "" {
			return nil, fmt.Errorf("aggregate batch filter cannot be empty")
		}
		filters[i] = "(" + filterSQL + ")"
		expr, err := conditionalAggregateExpression(request.Aggregate, fieldExpr, filters[i])
		if err != nil {
			return nil, err
		}
		selects[i] = expr + " AS " + quoteIdentifier("value_"+strconv.Itoa(i))
	}
	query := "SELECT " + strings.Join(selects, ", ") + " FROM " + c.tableIdentifier(requests[0].Table) +
		" PREWHERE " + strings.Join(filters, " OR ") + " FORMAT JSONEachRow"
	body, err := c.execute(ctx, query, nil)
	if err != nil {
		return nil, err
	}
	var row map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(body), &row); err != nil {
		return nil, fmt.Errorf("decode aggregate batch response: %w", err)
	}
	values := make([]any, len(requests))
	for i := range requests {
		values[i] = row["value_"+strconv.Itoa(i)]
	}
	return values, nil
}

func conditionalAggregateExpression(name, fieldExpr, condition string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "count":
		return "countIf((" + condition + ") AND isNotNull(" + fieldExpr + "))", nil
	case "count_distinct":
		return "uniqExactIf(" + fieldExpr + ", " + condition + ")", nil
	case "sum":
		return "coalesce(sumIf(" + fieldExpr + ", " + condition + "), 0)", nil
	case "avg", "min", "max":
		return strings.ToLower(strings.TrimSpace(name)) + "If(" + fieldExpr + ", " + condition + ")", nil
	default:
		return "", fmt.Errorf("unsupported aggregate %q", name)
	}
}

func (c clickHouseClient) execute(ctx context.Context, query string, body io.Reader) ([]byte, error) {
	endpoint, err := url.Parse(c.baseURL)
	if err != nil {
		return nil, err
	}
	params := endpoint.Query()
	params.Set("query", query)
	if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(query)), "SELECT") {
		// ClickHouse does not cancel HTTP SELECTs on disconnect by default. A
		// timed-out decision would otherwise release its application slot while
		// leaving the database query running, allowing abandoned work to grow
		// past the configured concurrency limit.
		params.Set("cancel_http_readonly_queries_on_client_close", "1")
		if c.serverQueryTimeout > 0 {
			params.Set("max_execution_time", strconv.FormatFloat(c.serverQueryTimeout.Seconds(), 'f', 3, 64))
			params.Set("timeout_before_checking_execution_speed", "0")
		}
	}
	endpoint.RawQuery = params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), body)
	if err != nil {
		return nil, err
	}
	if c.user != "" {
		req.SetBasicAuth(c.user, c.password)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ClickHouse request: %w", err)
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("ClickHouse status %d: %s", resp.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	return responseBody, nil
}

func decodeRecordRows(body []byte) ([]map[string]any, error) {
	rows := make([]map[string]any, 0)
	scanner := bufio.NewScanner(bytes.NewReader(body))
	for scanner.Scan() {
		var row map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	return rows, scanner.Err()
}

func aggregateExpression(name, fieldExpr string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "count":
		return "count(" + fieldExpr + ")", nil
	case "count_distinct":
		return "uniqExact(" + fieldExpr + ")", nil
	case "sum":
		return "coalesce(sum(" + fieldExpr + "), 0)", nil
	case "avg", "min", "max":
		return strings.ToLower(strings.TrimSpace(name)) + "(" + fieldExpr + ")", nil
	default:
		return "", fmt.Errorf("unsupported aggregate %q", name)
	}
}

func fieldExpression(field string, fields map[string]FieldContract) (string, error) {
	if !identifierPattern.MatchString(field) {
		return "", fmt.Errorf("invalid field %q", field)
	}
	if _, ok := fields[field]; !ok {
		return "", fmt.Errorf("field %s is not in the data model", field)
	}
	return quoteIdentifier(field), nil
}

func buildFilterSQL(filter AggregateFilter, fields map[string]FieldContract) (string, error) {
	kind := strings.ToLower(strings.TrimSpace(filter.Kind))
	if kind == "" || kind == "group" {
		op := strings.ToLower(strings.TrimSpace(filter.Operator))
		if op == "" {
			op = "and"
		}
		if op != "and" && op != "or" && op != "not" {
			return "", fmt.Errorf("unsupported group operator %q", op)
		}
		if op == "not" && len(filter.Children) != 1 {
			return "", fmt.Errorf("not requires one child")
		}
		parts := make([]string, 0, len(filter.Children))
		for _, child := range filter.Children {
			part, err := buildFilterSQL(child, fields)
			if err != nil {
				return "", err
			}
			if part != "" {
				parts = append(parts, "("+part+")")
			}
		}
		if op == "not" {
			if len(parts) != 1 {
				return "", fmt.Errorf("not requires one non-empty child")
			}
			return "NOT " + parts[0], nil
		}
		return strings.Join(parts, " "+strings.ToUpper(op)+" "), nil
	}
	if kind != "predicate" {
		return "", fmt.Errorf("unsupported filter kind %q", kind)
	}
	expr, err := fieldExpression(filter.Field, fields)
	if err != nil {
		return "", err
	}
	dataType := fields[filter.Field].DataType
	op := strings.ToLower(strings.TrimSpace(filter.Op))
	switch op {
	case "is_null":
		return expr + " IS NULL", nil
	case "is_not_null":
		return expr + " IS NOT NULL", nil
	case "starts_with":
		if dataType != "string" && dataType != "ip_address" {
			return "", fmt.Errorf("starts_with requires a string field")
		}
		return "startsWith(" + expr + ", " + quote(fmt.Sprint(filter.Value)) + ")", nil
	case "in":
		items, ok := filter.Value.([]any)
		if !ok || len(items) == 0 || len(items) > 100 {
			return "", fmt.Errorf("in requires 1 to 100 values")
		}
		values := make([]string, len(items))
		for i, item := range items {
			values[i] = typedLiteral(item, dataType)
		}
		return expr + " IN (" + strings.Join(values, ",") + ")", nil
	case "eq", "neq", "gt", "gte", "lt", "lte":
		operators := map[string]string{"eq": "=", "neq": "!=", "gt": ">", "gte": ">=", "lt": "<", "lte": "<="}
		return expr + " " + operators[op] + " " + typedLiteral(filter.Value, dataType), nil
	default:
		return "", fmt.Errorf("unsupported predicate operator %q", op)
	}
}

func typedLiteral(value any, dataType string) string {
	switch dataType {
	case "int", "float":
		if number, ok := value.(json.Number); ok {
			return number.String()
		}
		if number, ok := value.(float64); ok {
			return strconv.FormatFloat(number, 'g', -1, 64)
		}
	case "bool":
		if boolean, ok := value.(bool); ok && boolean {
			return "true"
		}
		if _, ok := value.(bool); ok {
			return "false"
		}
	case "timestamp":
		return "parseDateTime64BestEffort(" + quote(fmt.Sprint(value)) + ")"
	}
	return quote(fmt.Sprint(value))
}

func typedStringLiteral(value, dataType string) (string, error) {
	switch dataType {
	case "int":
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return "", fmt.Errorf("value must be an integer")
		}
		return strconv.FormatInt(parsed, 10), nil
	case "float":
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return "", fmt.Errorf("value must be a number")
		}
		return strconv.FormatFloat(parsed, 'g', -1, 64), nil
	case "bool":
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return "", fmt.Errorf("value must be a boolean")
		}
		return strconv.FormatBool(parsed), nil
	case "timestamp":
		if _, err := time.Parse(time.RFC3339Nano, value); err != nil {
			return "", fmt.Errorf("value must be an RFC3339 timestamp")
		}
		return "parseDateTime64BestEffort(" + quote(value) + ")", nil
	default:
		return quote(value), nil
	}
}

func clickHouseType(field FieldContract) (string, error) {
	var dataType string
	switch field.DataType {
	case "bool":
		dataType = "Bool"
	case "int":
		dataType = "Int64"
	case "float":
		dataType = "Float64"
	case "string", "ip_address":
		dataType = "String"
	case "timestamp":
		dataType = "DateTime64(3, 'UTC')"
	default:
		return "", fmt.Errorf("unsupported data type %q", field.DataType)
	}
	if field.Nullable {
		return "Nullable(" + dataType + ")", nil
	}
	return dataType, nil
}

func (c clickHouseClient) tableIdentifier(table TableContract) string {
	return c.database + ".`" + physicalTableName(table.TenantID, table.TableID) + "`"
}

func physicalTableName(tenantID, tableID string) string {
	return "event_" + strings.ReplaceAll(strings.ToLower(tenantID), "-", "") + "_" + strings.ReplaceAll(strings.ToLower(tableID), "-", "")
}

func selectFieldList(table TableContract) string {
	names := sortedFieldNames(table.Fields)
	columns := make([]string, len(names))
	for i, name := range names {
		columns[i] = quoteIdentifier(name)
	}
	return strings.Join(columns, ", ")
}

func sortedFieldNames(fields map[string]FieldContract) []string {
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func quoteIdentifier(value string) string { return "`" + value + "`" }

func normalizeClickHouseType(value string) string { return strings.ReplaceAll(value, " ", "") }

func quote(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `'`, `\'`)
	return "'" + value + "'"
}
