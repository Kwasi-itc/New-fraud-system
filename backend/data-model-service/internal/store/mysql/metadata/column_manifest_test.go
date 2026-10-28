package metadata_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

type columnMapping struct {
	SourceTable      string `json:"source_table"`
	SourceColumn     string `json:"source_column"`
	SourceType       string `json:"source_type"`
	SourceDefinition string `json:"source_definition"`
	SourceFile       string `json:"source_file"`
	TargetTable      string `json:"target_table"`
	TargetColumn     string `json:"target_column"`
	TargetType       string `json:"target_type"`
	TargetFile       string `json:"target_file"`
	Rule             string `json:"rule"`
}

type columnManifest struct {
	Format string `json:"format"`
	Status string `json:"status"`
	Scope  string `json:"scope"`
	Files  []struct {
		Path string `json:"path"`
		SHA  string `json:"sha256_lf"`
	} `json:"files"`
	Columns []columnMapping `json:"columns"`
	Objects []struct {
		SourceFile string `json:"source_file"`
		Kind       string `json:"kind"`
		Name       string `json:"name"`
		Target     string `json:"target"`
		Status     string `json:"status"`
	} `json:"source_objects"`
}

func readColumnManifest(t *testing.T) columnManifest {
	t.Helper()
	f, err := os.Open("../../../../testdata/metadata-column-manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var m columnManifest
	d := json.NewDecoder(f)
	d.DisallowUnknownFields()
	if err := d.Decode(&m); err != nil {
		t.Fatal(err)
	}
	if m.Format != "data-model-schema-mapping/v1" || m.Status != "needs_review" || m.Scope == "" {
		t.Fatal("unsupported manifest/review state")
	}
	return m
}

// This inventory understands only the checked-in metadata migration grammar.
// Source hashes also cover constraints, routines, indexes and down migrations;
// changing them requires explicit manifest review, not automatic regeneration.
func TestMetadataColumnManifest(t *testing.T) {
	m := readColumnManifest(t)
	root := "../../../.."
	bound := map[string]string{}
	for _, f := range m.Files {
		if !filepath.IsLocal(f.Path) || f.Path != filepath.ToSlash(filepath.Clean(f.Path)) {
			t.Fatal("unsafe manifest path", f.Path)
		}
		if _, exists := bound[f.Path]; exists {
			t.Fatal("duplicate file", f.Path)
		}
		b, err := os.ReadFile(filepath.Join(root, f.Path))
		if err != nil {
			t.Fatal(err)
		}
		text := strings.ReplaceAll(string(b), "\r\n", "\n")
		hash := sha256.Sum256([]byte(text))
		if hex.EncodeToString(hash[:]) != f.SHA {
			t.Errorf("manifest source drift: %s", f.Path)
		}
		bound[f.Path] = text
	}
	for _, pattern := range []string{"internal/migrations/metadata/*.sql", "internal/store/mysql/metadata/sql/*.sql", "internal/store/mysql/exactkeys/sql/*.sql"} {
		paths, err := filepath.Glob(filepath.Join(root, pattern))
		if err != nil || len(paths) == 0 {
			t.Fatal("missing source inventory", pattern, err)
		}
		for _, path := range paths {
			rel, err := filepath.Rel(root, path)
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := bound[filepath.ToSlash(rel)]; !ok {
				t.Errorf("unbound schema file: %s", path)
			}
		}
	}
	for _, path := range []string{"internal/store/mysql/metadata/schema.go", "internal/store/mysql/exactkeys/schema.go", "internal/store/mysql/installation/delivery_retry_upgrade.go", "internal/store/mysql/installation/creation_receipt_upgrade.go", "internal/store/mysql/installation/owner_result_upgrade.go"} {
		if _, ok := bound[path]; !ok {
			t.Errorf("missing generated schema binding: %s", path)
		}
	}
	named := regexp.MustCompile(`(?m)^CREATE (?:OR REPLACE )?(?:UNIQUE )?(FUNCTION|TRIGGER|INDEX) (?:IF NOT EXISTS )?([a-z_.]+)`)
	objects := map[string]bool{}
	for path, sql := range bound {
		if strings.HasPrefix(path, "internal/migrations/metadata/") && strings.HasSuffix(path, ".up.sql") {
			for _, object := range named.FindAllStringSubmatch(sql, -1) {
				objects[path+":"+object[1]+":"+object[2]] = false
			}
		}
	}
	for _, o := range m.Objects {
		key := o.SourceFile + ":" + o.Kind + ":" + o.Name
		seen, ok := objects[key]
		if !ok || seen {
			t.Errorf("duplicate or unknown source object: %s", key)
		}
		objects[key] = true
		path, symbol, ok := strings.Cut(o.Target, "#")
		if !ok || !filepath.IsLocal(path) || symbol == "" {
			t.Fatalf("invalid target reference: %s", o.Target)
		}
		data, err := os.ReadFile(filepath.Join(root, path))
		if err != nil || !strings.Contains(string(data), symbol) {
			t.Errorf("missing replacement reference: %s: %v", o.Target, err)
		}
		status := "staged_owner_integration_incomplete"
		if o.Kind == "INDEX" {
			status = "staged_structural"
		}
		if o.Status != status {
			t.Errorf("review replacement acceptance separately: %s", key)
		}
	}
	for key, seen := range objects {
		if !seen {
			t.Errorf("unmapped source object: %s", key)
		}
	}
	sourceColumns := map[string]string{}
	declarations := map[string]string{}
	create := regexp.MustCompile(`(?s)CREATE TABLE (?:IF NOT EXISTS )?(core\.[a-z_]+)\s*\((.*?)\n\);`)
	column := regexp.MustCompile(`(?m)^\s*([a-z_]+)\s+((?:UUID|TEXT|TIMESTAMPTZ|BOOLEAN|INTEGER|BIGINT|JSONB)[^\n]*)`)
	alter := regexp.MustCompile(`(?s)ALTER TABLE (core\.[a-z_]+)\s+(ADD COLUMN .*?);`)
	add := regexp.MustCompile(`ADD COLUMN (?:IF NOT EXISTS )?([a-z_]+)\s+([^\n]+)`)
	for path, sql := range bound {
		if !strings.HasPrefix(path, "internal/migrations/metadata/") || !strings.HasSuffix(path, ".up.sql") {
			continue
		}
		for _, table := range create.FindAllStringSubmatch(sql, -1) {
			for _, c := range column.FindAllStringSubmatch(table[2], -1) {
				sourceColumns[table[1]+"."+c[1]] = path
				declarations[table[1]+"."+c[1]] = strings.TrimRight(strings.TrimSpace(c[2]), ",;")
			}
		}
		for _, table := range alter.FindAllStringSubmatch(sql, -1) {
			for _, c := range add.FindAllStringSubmatch(table[2], -1) {
				sourceColumns[table[1]+"."+c[1]] = path
				declarations[table[1]+"."+c[1]] = strings.TrimRight(strings.TrimSpace(c[2]), ",;")
			}
		}
	}
	seen := map[string]bool{}
	for _, c := range m.Columns {
		key := c.SourceTable + "." + c.SourceColumn
		if seen[key] || sourceColumns[key] != c.SourceFile {
			t.Errorf("duplicate or unrecognized source mapping: %s", key)
		}
		seen[key] = true
		if c.SourceDefinition == "" || c.SourceDefinition != declarations[key] || !strings.HasPrefix(c.SourceDefinition, c.SourceType) {
			t.Errorf("missing source declaration: %s", key)
		}
		target := bound[c.TargetFile]
		if !strings.Contains(target, "CREATE TABLE {{database}}."+c.TargetTable+" (") || !regexp.MustCompile(`\b`+regexp.QuoteMeta(c.TargetColumn)+`\s+`+regexp.QuoteMeta(c.TargetType)+`(?:\s|,)`).MatchString(target) {
			t.Errorf("target column/type missing: %s", key)
		}
		switch c.Rule {
		case "scalar":
			allowed := map[string][]string{"UUID": {"BINARY(16)"}, "TIMESTAMPTZ": {"DATETIME(6)"}, "TEXT": {"LONGTEXT", "VARCHAR(20)"}, "BOOLEAN": {"BOOLEAN"}, "INTEGER": {"INT"}, "BIGINT": {"BIGINT"}, "JSONB": {"JSON"}}
			valid := false
			for _, typ := range allowed[c.SourceType] {
				valid = valid || typ == c.TargetType
			}
			if !valid {
				t.Errorf("unsupported scalar conversion: %s", key)
			}
		case "ordered_rows":
			if c.SourceType != "UUID[]" || c.TargetType != "BINARY(16)" || !strings.Contains(target, "ordinal INT") {
				t.Errorf("invalid ordered relation: %s", key)
			}
		case "ordered_json":
			if key != "core.index_jobs.columns" || c.SourceType != "TEXT[]" || c.TargetType != "JSON" {
				t.Errorf("invalid opaque array: %s", key)
			}
		case "owner_publication_version":
			if key != "core.ingestion_contracts.version" || c.TargetType != "BINARY(16)" {
				t.Errorf("invalid owner version: %s", key)
			}
		default:
			t.Errorf("unknown conversion rule: %s", key)
		}
	}
	for key := range sourceColumns {
		if !seen[key] {
			t.Errorf("unmapped source column: %s", key)
		}
	}
	if len(sourceColumns) != 101 || len(seen) != 101 {
		t.Fatalf("review source inventory change: source=%d mapped=%d", len(sourceColumns), len(seen))
	}
}

// Account for full encoded key bytes, not character counts or prefix keys.
// This is a structural ceiling check, not a query-plan or throughput benchmark.
func TestMetadataIndexByteBudgets(t *testing.T) {
	db, database := metadataDB(t)
	rows, err := db.QueryContext(context.Background(), `SELECT s.TABLE_NAME,s.INDEX_NAME,c.DATA_TYPE,c.CHARACTER_OCTET_LENGTH,s.SUB_PART,c.IS_NULLABLE
FROM information_schema.STATISTICS s JOIN information_schema.COLUMNS c
ON c.TABLE_SCHEMA=s.TABLE_SCHEMA AND c.TABLE_NAME=s.TABLE_NAME AND c.COLUMN_NAME=s.COLUMN_NAME
WHERE s.TABLE_SCHEMA=? ORDER BY s.TABLE_NAME,s.INDEX_NAME,s.SEQ_IN_INDEX LIMIT 1001`, database)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	type budget struct {
		bytes, parts int
		table, index string
	}
	indexes := map[string]budget{}
	count := 0
	for rows.Next() {
		var table, index, typ, nullable string
		var octets, prefix sql.NullInt64
		if err := rows.Scan(&table, &index, &typ, &octets, &prefix, &nullable); err != nil {
			t.Fatal(err)
		}
		count++
		if prefix.Valid {
			t.Errorf("prefix key is not exact: %s.%s", table, index)
		}
		width := 0
		switch typ {
		case "binary", "char":
			width = int(octets.Int64)
		case "varbinary", "varchar":
			width = int(octets.Int64) + 2
		case "tinyint":
			width = 1
		case "int":
			width = 4
		case "bigint", "datetime", "double":
			width = 8
		default:
			t.Fatalf("review key encoding for %s.%s: %s", table, index, typ)
		}
		if width <= 0 {
			t.Fatal("unbounded key encoding", table, index)
		}
		// Conservatively charge a whole byte per nullable component.
		if nullable == "YES" {
			width++
		}
		key := table + "." + index
		b := indexes[key]
		b.bytes += width
		b.parts++
		b.table = table
		b.index = index
		indexes[key] = b
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if count == 0 || count >= 1001 {
		t.Fatal("index inventory budget", count)
	}
	maxBytes, maxLeaf := 0, 0
	for key, b := range indexes {
		if b.parts > 16 || b.bytes > 3072 {
			t.Errorf("key budget exceeded: %s %+v", key, b)
		}
		if b.bytes > maxBytes {
			maxBytes = b.bytes
		}
		leaf := b.bytes
		if b.index != "PRIMARY" {
			primary, ok := indexes[b.table+".PRIMARY"]
			if !ok {
				t.Fatal("missing explicit primary key", b.table)
			}
			// Upper bound: even charge primary parts already present in this key.
			leaf += primary.bytes
		}
		if leaf > maxLeaf {
			maxLeaf = leaf
		}
	}
	t.Logf("%d indexes / %d components; maximum conservative key bytes=%d, secondary key plus primary bytes=%d (excludes record/page overhead)", len(indexes), count, maxBytes, maxLeaf)
}

func TestMetadataColumnManifestInstalled(t *testing.T) {
	m := readColumnManifest(t)
	db, database := metadataDB(t)
	rows, err := db.QueryContext(context.Background(), "SELECT TABLE_NAME,COLUMN_NAME,COLUMN_TYPE,IS_NULLABLE FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=? ORDER BY TABLE_NAME,ORDINAL_POSITION LIMIT 1001", database)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	type shape struct{ typ, nullable string }
	actual := map[string]shape{}
	for rows.Next() {
		var table, col string
		var s shape
		if err := rows.Scan(&table, &col, &s.typ, &s.nullable); err != nil {
			t.Fatal(err)
		}
		actual[table+"."+col] = s
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(actual) >= 1001 {
		t.Fatal("manifest verification row budget exceeded")
	}
	for _, c := range m.Columns {
		key := c.TargetTable + "." + c.TargetColumn
		s, ok := actual[key]
		want := strings.ToLower(c.TargetType)
		if want == "boolean" {
			want = "tinyint(1)"
		}
		if !ok || s.typ != want {
			t.Errorf("installed type %s: got %q want %q", key, s.typ, want)
		}
		required := strings.Contains(c.SourceDefinition, "NOT NULL") || strings.Contains(c.SourceDefinition, "PRIMARY KEY")
		if (s.nullable == "NO") != required {
			t.Errorf("installed nullability %s: %s", key, s.nullable)
		}
	}
}
