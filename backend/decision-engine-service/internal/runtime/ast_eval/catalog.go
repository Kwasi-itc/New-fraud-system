package ast_eval

import domainast "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/ast"

type FunctionArgument struct {
	Name        string
	Kind        string
	Required    bool
	Description string
}

type FunctionDescriptor struct {
	Name              string
	Category          string
	Description       string
	ReturnType        domainast.ValueType
	PositionalArity   *int
	SupportsNamedArgs bool
	Arguments         []FunctionArgument
	RequiresModel     bool
	RequiresDataRead  bool
	RequiresPlatform  bool
	Example           string
}

func SupportedFunctionCatalog() []FunctionDescriptor {
	return []FunctionDescriptor{
		{Name: "constant", Category: "literals", Description: "Returns the provided constant value.", Example: `{"function":"constant","constant":42}`},
		{
			Name:              "field_ref",
			Category:          "record fields",
			Description:       "Reads one field from the current record payload.",
			ReturnType:        domainast.ValueTypeUnknown,
			SupportsNamedArgs: true,
			Arguments:         []FunctionArgument{{Name: "field", Kind: "string", Required: true, Description: "Field name on the current record."}},
			Example:           `{"function":"field_ref","named_children":{"field":{"constant":"amount"}}}`,
		},
		booleanFunc("and", "Returns true only if every child is true.", nil, `{"function":"and","children":[{"function":"eq","children":[{"constant":"a"},{"constant":"a"}]},{"constant":true}]}`),
		booleanFunc("or", "Returns true if any child is true.", nil, `{"function":"or","children":[{"constant":false},{"constant":true}]}`),
		booleanFunc("not", "Negates one boolean child.", intPtr(1), `{"function":"not","children":[{"constant":false}]}`),
		comparisonFunc("eq", "Checks whether two operands are equal."),
		comparisonFunc("neq", "Checks whether two operands are not equal."),
		comparisonFunc("gt", "Checks whether the left operand is greater than the right operand."),
		comparisonFunc("gte", "Checks whether the left operand is greater than or equal to the right operand."),
		comparisonFunc("lt", "Checks whether the left operand is less than the right operand."),
		comparisonFunc("lte", "Checks whether the left operand is less than or equal to the right operand."),
		{Name: "contains", Category: "string and list", Description: "Checks whether a string contains a substring or a list contains a value.", ReturnType: domainast.ValueTypeBool, PositionalArity: intPtr(2), Example: `{"function":"contains","children":[{"constant":"hello world"},{"constant":"world"}]}`},
		{Name: "StringNotContain", Category: "marble compatibility", Description: "Returns true when the left string does not contain the right substring.", ReturnType: domainast.ValueTypeBool, PositionalArity: intPtr(2), Example: `{"name":"StringNotContain","children":[{"constant":"hello world"},{"constant":"fraud"}]}`},
		{Name: "ContainsAnyOf", Category: "marble compatibility", Description: "Returns true when the left string contains at least one string from the right list.", ReturnType: domainast.ValueTypeBool, PositionalArity: intPtr(2), Example: `{"name":"ContainsAnyOf","children":[{"constant":"sanction hit"},{"name":"List","children":[{"constant":"pep"},{"constant":"hit"}]}]}`},
		{Name: "ContainsNoneOf", Category: "marble compatibility", Description: "Returns true when the left string contains none of the strings from the right list.", ReturnType: domainast.ValueTypeBool, PositionalArity: intPtr(2), Example: `{"name":"ContainsNoneOf","children":[{"constant":"approved"},{"name":"List","children":[{"constant":"review"},{"constant":"decline"}]}]}`},
		{Name: "in", Category: "string and list", Description: "Checks whether the left value exists inside the right list.", ReturnType: domainast.ValueTypeBool, PositionalArity: intPtr(2), Example: `{"function":"in","children":[{"constant":"review"},{"constant":["approve","review","decline"]}]}`},
		{Name: "starts_with", Category: "string and list", Description: "Checks whether a string starts with a prefix.", ReturnType: domainast.ValueTypeBool, PositionalArity: intPtr(2), Example: `{"function":"starts_with","children":[{"constant":"txn_1001"},{"constant":"txn_"}]}`},
		{Name: "ends_with", Category: "string and list", Description: "Checks whether a string ends with a suffix.", ReturnType: domainast.ValueTypeBool, PositionalArity: intPtr(2), Example: `{"function":"ends_with","children":[{"constant":"user@example.com"},{"constant":".com"}]}`},
		{Name: "lower", Category: "transforms", Description: "Lowercases one string child.", ReturnType: domainast.ValueTypeString, PositionalArity: intPtr(1), Example: `{"function":"lower","children":[{"constant":"ABC"}]}`},
		{Name: "upper", Category: "transforms", Description: "Uppercases one string child.", ReturnType: domainast.ValueTypeString, PositionalArity: intPtr(1), Example: `{"function":"upper","children":[{"constant":"abc"}]}`},
		{Name: "is_null", Category: "null handling", Description: "Returns true if the child resolves to null.", ReturnType: domainast.ValueTypeBool, PositionalArity: intPtr(1), Example: `{"function":"is_null","children":[{"function":"field_ref","named_children":{"field":{"constant":"email"}}}]}`},
		{Name: "IsEmpty", Category: "marble compatibility", Description: "Returns true when the child resolves to null or an empty string.", ReturnType: domainast.ValueTypeBool, PositionalArity: intPtr(1), Example: `{"name":"IsEmpty","children":[{"name":"Payload","children":[{"constant":"middle_name"}]}]}`},
		{Name: "IsNotEmpty", Category: "marble compatibility", Description: "Returns true when the child resolves to a non-empty value.", ReturnType: domainast.ValueTypeBool, PositionalArity: intPtr(1), Example: `{"name":"IsNotEmpty","children":[{"name":"Payload","children":[{"constant":"email"}]}]}`},
		{Name: "coalesce", Category: "null handling", Description: "Returns the first non-null child.", ReturnType: domainast.ValueTypeUnknown, Example: `{"function":"coalesce","children":[{"function":"field_ref","named_children":{"field":{"constant":"nickname"}}},{"function":"field_ref","named_children":{"field":{"constant":"full_name"}}}]}`},
		arithmeticFunc("add", "Adds two numeric operands."),
		arithmeticFunc("subtract", "Subtracts the right numeric operand from the left operand."),
		arithmeticFunc("multiply", "Multiplies two numeric operands."),
		arithmeticFunc("divide", "Divides the left numeric operand by the right operand."),
		{
			Name:              "in_custom_list",
			Category:          "platform helpers",
			Description:       "Checks whether a value exists in a tenant custom list.",
			ReturnType:        domainast.ValueTypeBool,
			SupportsNamedArgs: true,
			Arguments: []FunctionArgument{
				{Name: "list", Kind: "string", Required: true, Description: "Custom list name."},
				{Name: "value", Kind: "string", Required: true, Description: "Value to check."},
			},
			RequiresPlatform: true,
			Example:          `{"function":"in_custom_list","named_children":{"list":{"constant":"blocked_emails"},"value":{"function":"field_ref","named_children":{"field":{"constant":"email"}}}}}`,
		},
		{
			Name:              "record_has_tag",
			Category:          "platform helpers",
			Description:       "Checks whether the current record has a tenant tag.",
			ReturnType:        domainast.ValueTypeBool,
			SupportsNamedArgs: true,
			Arguments:         []FunctionArgument{{Name: "tag", Kind: "string", Required: true, Description: "Tag name to check."}},
			RequiresPlatform:  true,
			Example:           `{"function":"record_has_tag","named_children":{"tag":{"constant":"vip"}}}`,
		},
		{
			Name:             "record_risk_level",
			Category:         "platform helpers",
			Description:      "Returns the current record risk snapshot level if present.",
			ReturnType:       domainast.ValueTypeString,
			RequiresPlatform: true,
			Example:          `{"function":"record_risk_level"}`,
		},
		{
			Name:              "has_ip_flag",
			Category:          "platform helpers",
			Description:       "Checks whether an IP address has a tenant-scoped flag.",
			ReturnType:        domainast.ValueTypeBool,
			SupportsNamedArgs: true,
			Arguments: []FunctionArgument{
				{Name: "ip", Kind: "string", Required: true, Description: "IP address to inspect."},
				{Name: "flag", Kind: "string", Required: true, Description: "Flag name to check."},
			},
			RequiresPlatform: true,
			Example:          `{"function":"has_ip_flag","named_children":{"ip":{"function":"field_ref","named_children":{"field":{"constant":"ip_address"}}},"flag":{"constant":"tor_exit_node"}}}`,
		},
		{
			Name:              "past_decision_count",
			Category:          "decision history",
			Description:       "Counts past decisions for the current object, optionally filtered by outcome.",
			ReturnType:        domainast.ValueTypeNumber,
			SupportsNamedArgs: true,
			Arguments:         []FunctionArgument{{Name: "outcome", Kind: "string", Required: false, Description: "Optional outcome filter."}},
			RequiresDataRead:  true,
			Example:           `{"function":"past_decision_count","named_children":{"outcome":{"constant":"review"}}}`,
		},
		{
			Name:              "related_count",
			Category:          "related data",
			Description:       "Counts records of another object type, optionally restricted to records whose field equals a provided value.",
			ReturnType:        domainast.ValueTypeNumber,
			SupportsNamedArgs: true,
			Arguments: []FunctionArgument{
				{Name: "object_type", Kind: "string", Required: true, Description: "Related object type to scan."},
				{Name: "field", Kind: "string", Required: true, Description: "Field to count or compare on."},
				{Name: "equals", Kind: "any", Required: false, Description: "Optional comparison value for the related field."},
			},
			RequiresDataRead: true,
			Example:          `{"function":"related_count","named_children":{"object_type":{"constant":"transactions"},"field":{"constant":"customer_id"},"equals":{"function":"field_ref","named_children":{"field":{"constant":"customer_id"}}}}}`,
		},
		{
			Name:              "related_field",
			Category:          "related data",
			Description:       "Traverses a named link path and returns one field from the resolved related record.",
			ReturnType:        domainast.ValueTypeUnknown,
			SupportsNamedArgs: true,
			Arguments: []FunctionArgument{
				{Name: "path", Kind: "string", Required: true, Description: "Dot-separated links_to_single path."},
				{Name: "field", Kind: "string", Required: true, Description: "Field to read on the resolved related record."},
			},
			RequiresModel:    true,
			RequiresDataRead: true,
			Example:          `{"function":"related_field","named_children":{"path":{"constant":"account.customer"},"field":{"constant":"country"}}}`,
		},
		{
			Name:            "Payload",
			Category:        "marble compatibility",
			Description:     "Marble-compatible alias for reading one field from the trigger payload.",
			ReturnType:      domainast.ValueTypeUnknown,
			PositionalArity: intPtr(1),
			Example:         `{"name":"Payload","children":[{"constant":"amount"}]}`,
		},
		{
			Name:              "DatabaseAccess",
			Category:          "marble compatibility",
			Description:       "Marble-compatible database field access using a path of relationship names.",
			ReturnType:        domainast.ValueTypeUnknown,
			SupportsNamedArgs: true,
			Arguments: []FunctionArgument{
				{Name: "tableName", Kind: "string", Required: true, Description: "Trigger table name."},
				{Name: "fieldName", Kind: "string", Required: true, Description: "Field to read from the resolved record."},
				{Name: "path", Kind: "list", Required: true, Description: "Relationship path from the trigger record to the target record."},
			},
			RequiresModel:    true,
			RequiresDataRead: true,
			Example:          `{"name":"DatabaseAccess","named_children":{"tableName":{"constant":"transactions"},"fieldName":{"constant":"status"},"path":{"constant":["account"]}}}`,
		},
		{
			Name:        "List",
			Category:    "marble compatibility",
			Description: "Marble-compatible list literal used to hold Filter nodes or plain values.",
			ReturnType:  domainast.ValueTypeList,
			Example:     `{"name":"List","children":[{"constant":"a"},{"constant":"b"}]}`,
		},
		{
			Name:              "Filter",
			Category:          "marble compatibility",
			Description:       "Marble-compatible typed filter descriptor used inside Aggregator filters.",
			ReturnType:        domainast.ValueTypeObject,
			SupportsNamedArgs: true,
			Arguments: []FunctionArgument{
				{Name: "tableName", Kind: "string", Required: true, Description: "Table the filter targets."},
				{Name: "fieldName", Kind: "string", Required: true, Description: "Field the filter targets."},
				{Name: "operator", Kind: "string", Required: true, Description: "Filter operator such as =, <, IsEmpty, or StringStartsWith."},
				{Name: "value", Kind: "any", Required: false, Description: "Comparison value, omitted for unary operators such as IsEmpty."},
			},
			RequiresModel: true,
			Example:       `{"name":"Filter","named_children":{"tableName":{"constant":"transactions"},"fieldName":{"constant":"created_at"},"operator":{"constant":"<"},"value":{"name":"TimeAdd","named_children":{"timestampField":{"name":"Payload","children":[{"constant":"created_at"}]},"duration":{"constant":"PT5M"},"sign":{"constant":"-"}}}}}`,
		},
		{
			Name:              "Aggregator",
			Category:          "marble compatibility",
			Description:       "Marble-compatible aggregation node that scans one table, applies Filter nodes, then computes an aggregate over one field.",
			ReturnType:        domainast.ValueTypeUnknown,
			SupportsNamedArgs: true,
			Arguments: []FunctionArgument{
				{Name: "tableName", Kind: "string", Required: true, Description: "Table to scan."},
				{Name: "fieldName", Kind: "string", Required: true, Description: "Field aggregated across matching records."},
				{Name: "aggregator", Kind: "string", Required: true, Description: "Aggregator such as COUNT, COUNT_DISTINCT, SUM, AVG, MIN, MAX, STDDEV, PCTILE, or MEDIAN."},
				{Name: "filters", Kind: "list", Required: false, Description: "Optional List of Filter nodes applied with AND semantics."},
				{Name: "label", Kind: "string", Required: false, Description: "Optional human-readable label kept for authoring parity."},
				{Name: "percentile", Kind: "number", Required: false, Description: "Required for PCTILE."},
			},
			RequiresModel:    true,
			RequiresDataRead: true,
			Example:          `{"name":"Aggregator","named_children":{"tableName":{"constant":"transactions"},"fieldName":{"constant":"amount"},"aggregator":{"constant":"SUM"},"filters":{"name":"List","children":[{"name":"Filter","named_children":{"tableName":{"constant":"transactions"},"fieldName":{"constant":"owner_id"},"operator":{"constant":"="},"value":{"name":"Payload","children":[{"constant":"owner_id"}]}}}]}}}`,
		},
		{
			Name:              "TimeAdd",
			Category:          "marble compatibility",
			Description:       "Marble-compatible time arithmetic using ISO-8601-like durations such as PT5M.",
			ReturnType:        domainast.ValueTypeTimestamp,
			SupportsNamedArgs: true,
			Arguments: []FunctionArgument{
				{Name: "timestampField", Kind: "timestamp", Required: true, Description: "Base timestamp."},
				{Name: "duration", Kind: "string", Required: true, Description: "Duration text such as PT5M or 24h."},
				{Name: "sign", Kind: "string", Required: true, Description: "Either + or -."},
			},
			Example: `{"name":"TimeAdd","named_children":{"timestampField":{"name":"Payload","children":[{"constant":"created_at"}]},"duration":{"constant":"PT5M"},"sign":{"constant":"-"}}}`,
		},
		{
			Name:        "TimeNow",
			Category:    "marble compatibility",
			Description: "Marble-compatible current timestamp function.",
			ReturnType:  domainast.ValueTypeTimestamp,
			Example:     `{"name":"TimeNow"}`,
		},
		{
			Name:            "ParseTime",
			Category:        "marble compatibility",
			Description:     "Marble-compatible parser for RFC3339 timestamp strings.",
			ReturnType:      domainast.ValueTypeTimestamp,
			PositionalArity: intPtr(1),
			Example:         `{"name":"ParseTime","children":[{"constant":"2026-05-26T12:00:00Z"}]}`,
		},
		{
			Name:              "TimestampExtract",
			Category:          "marble compatibility",
			Description:       "Extracts one part of a timestamp such as year, month, day_of_month, day_of_week, or hour.",
			ReturnType:        domainast.ValueTypeNumber,
			SupportsNamedArgs: true,
			Arguments: []FunctionArgument{
				{Name: "timestamp", Kind: "timestamp", Required: true, Description: "Timestamp to inspect."},
				{Name: "part", Kind: "string", Required: true, Description: "One of year, month, day_of_month, day_of_week, or hour."},
			},
			Example: `{"name":"TimestampExtract","named_children":{"timestamp":{"name":"Payload","children":[{"constant":"created_at"}]},"part":{"constant":"hour"}}}`,
		},
		{
			Name:              "FuzzyMatch",
			Category:          "marble compatibility",
			Description:       "Returns a similarity score between two strings using the selected algorithm.",
			ReturnType:        domainast.ValueTypeNumber,
			PositionalArity:   intPtr(2),
			SupportsNamedArgs: true,
			Arguments:         []FunctionArgument{{Name: "algorithm", Kind: "string", Required: true, Description: "Similarity algorithm such as ratio or bag_of_words_similarity."}},
			Example:           `{"name":"FuzzyMatch","named_children":{"algorithm":{"constant":"ratio"}},"children":[{"constant":"John Smith"},{"constant":"Jon Smyth"}]}`,
		},
		{
			Name:              "FuzzyMatchAnyOf",
			Category:          "marble compatibility",
			Description:       "Returns the highest similarity score between one string and any string in a list.",
			ReturnType:        domainast.ValueTypeNumber,
			PositionalArity:   intPtr(2),
			SupportsNamedArgs: true,
			Arguments:         []FunctionArgument{{Name: "algorithm", Kind: "string", Required: true, Description: "Similarity algorithm such as ratio or bag_of_words_similarity."}},
			Example:           `{"name":"FuzzyMatchAnyOf","named_children":{"algorithm":{"constant":"bag_of_words_similarity"}},"children":[{"constant":"John Smith"},{"name":"List","children":[{"constant":"Jon Smyth"},{"constant":"Alice Brown"}]}]}`,
		},
		{
			Name:              "FuzzyMatchOptions",
			Category:          "marble compatibility",
			Description:       "Builds the fuzzy-match filter option object used by monitoring features.",
			ReturnType:        domainast.ValueTypeObject,
			SupportsNamedArgs: true,
			Arguments: []FunctionArgument{
				{Name: "algorithm", Kind: "string", Required: true, Description: "Database-side fuzzy match algorithm."},
				{Name: "threshold", Kind: "number", Required: true, Description: "Similarity threshold from 0 to 100."},
				{Name: "value", Kind: "string", Required: true, Description: "Comparison text."},
			},
			Example: `{"name":"FuzzyMatchOptions","named_children":{"algorithm":{"constant":"bag_of_words_similarity_db"},"threshold":{"constant":85},"value":{"constant":"John Smith"}}}`,
		},
		{
			Name:              "IsMultipleOf",
			Category:          "marble compatibility",
			Description:       "Returns true when value is an exact multiple of divider.",
			ReturnType:        domainast.ValueTypeBool,
			SupportsNamedArgs: true,
			Arguments: []FunctionArgument{
				{Name: "value", Kind: "number", Required: true, Description: "Number to test."},
				{Name: "divider", Kind: "number", Required: true, Description: "Integer divisor."},
			},
			Example: `{"name":"IsMultipleOf","named_children":{"value":{"name":"Payload","children":[{"constant":"amount"}]},"divider":{"constant":5}}}`,
		},
		{
			Name:              "StringTemplate",
			Category:          "marble compatibility",
			Description:       "Interpolates named variables into a template string using %variable% placeholders.",
			ReturnType:        domainast.ValueTypeString,
			PositionalArity:   intPtr(1),
			SupportsNamedArgs: true,
			Arguments:         []FunctionArgument{{Name: "variable_name", Kind: "any", Required: false, Description: "Template variable values are supplied as named children."}},
			Example:           `{"name":"StringTemplate","children":[{"constant":"Hello %first_name%"}],"named_children":{"first_name":{"constant":"Kwasi"}}}`,
		},
		{
			Name:              "StringConcat",
			Category:          "marble compatibility",
			Description:       "Concatenates string and numeric children, optionally inserting a separator.",
			ReturnType:        domainast.ValueTypeString,
			SupportsNamedArgs: true,
			Arguments: []FunctionArgument{
				{Name: "with_separator", Kind: "bool", Required: false, Description: "Whether to place a separator between rendered children."},
				{Name: "separator", Kind: "string", Required: false, Description: "Separator text when with_separator is true."},
			},
			Example: `{"name":"StringConcat","named_children":{"with_separator":{"constant":true},"separator":{"constant":" "}},"children":[{"constant":"risk"},{"constant":"alert"}]}`,
		},
		{
			Name:              "ScoreComputation",
			Category:          "marble compatibility",
			Description:       "Wraps one boolean condition into a score branch with modifier and optional floor.",
			ReturnType:        domainast.ValueTypeObject,
			PositionalArity:   intPtr(1),
			SupportsNamedArgs: true,
			Arguments: []FunctionArgument{
				{Name: "modifier", Kind: "number", Required: true, Description: "Score modifier when the child condition is true."},
				{Name: "floor", Kind: "number", Required: false, Description: "Optional minimum floor applied by the branch."},
			},
			Example: `{"name":"ScoreComputation","named_children":{"modifier":{"constant":20},"floor":{"constant":50}},"children":[{"name":">","children":[{"name":"Payload","children":[{"constant":"amount"}]},{"constant":1000}]}]}`,
		},
		{
			Name:              "Switch",
			Category:          "marble compatibility",
			Description:       "Chooses the first triggered ScoreComputation branch, or fallback/default when none match.",
			ReturnType:        domainast.ValueTypeObject,
			SupportsNamedArgs: true,
			Arguments: []FunctionArgument{
				{Name: "field", Kind: "any", Required: true, Description: "Presence guard used before checking branches."},
				{Name: "fallback", Kind: "object", Required: false, Description: "Optional fallback score result."},
			},
			Example: `{"name":"Switch","named_children":{"field":{"name":"Payload","children":[{"constant":"amount"}]}},"children":[{"name":"ScoreComputation","named_children":{"modifier":{"constant":10}},"children":[{"name":">","children":[{"name":"Payload","children":[{"constant":"amount"}]},{"constant":100}]}]}]}`,
		},
		{
			Name:              "related_records",
			Category:          "related data",
			Description:       "Loads related records as a list so later functions can filter, aggregate, and group them.",
			ReturnType:        domainast.ValueTypeList,
			SupportsNamedArgs: true,
			Arguments: []FunctionArgument{
				{Name: "object_type", Kind: "string", Required: true, Description: "Related object type to load."},
				{Name: "match_field", Kind: "string", Required: false, Description: "Optional field on the related record to compare."},
				{Name: "equals", Kind: "any", Required: false, Description: "Optional comparison value used with match_field."},
				{Name: "timestamp_field", Kind: "string", Required: false, Description: "Optional timestamp field used for time-window filtering."},
				{Name: "within_hours", Kind: "number", Required: false, Description: "Optional lookback window in hours, used with timestamp_field."},
				{Name: "limit", Kind: "number", Required: false, Description: "Optional maximum number of records to scan."},
			},
			RequiresDataRead: true,
			Example:          `{"function":"related_records","named_children":{"object_type":{"constant":"transactions"},"match_field":{"constant":"customer_id"},"equals":{"function":"field_ref","named_children":{"field":{"constant":"customer_id"}}},"timestamp_field":{"constant":"created_at"},"within_hours":{"constant":24}}}`,
		},
		{
			Name:              "filter_eq",
			Category:          "collection transforms",
			Description:       "Filters a list of object records to only items whose field equals the provided value.",
			ReturnType:        domainast.ValueTypeList,
			SupportsNamedArgs: true,
			Arguments: []FunctionArgument{
				{Name: "items", Kind: "list", Required: true, Description: "List of object records to filter."},
				{Name: "field", Kind: "string", Required: true, Description: "Field name to compare on each object."},
				{Name: "value", Kind: "any", Required: true, Description: "Expected value for the selected field."},
			},
			Example: `{"function":"filter_eq","named_children":{"items":{"function":"related_records","named_children":{"object_type":{"constant":"transactions"}}},"field":{"constant":"status"},"value":{"constant":"review"}}}`,
		},
		{
			Name:              "map_field",
			Category:          "collection transforms",
			Description:       "Projects one field from each object in a list into a plain list of values.",
			ReturnType:        domainast.ValueTypeList,
			SupportsNamedArgs: true,
			Arguments: []FunctionArgument{
				{Name: "items", Kind: "list", Required: true, Description: "List of object records to project."},
				{Name: "field", Kind: "string", Required: true, Description: "Field name to extract from each object."},
			},
			Example: `{"function":"map_field","named_children":{"items":{"function":"related_records","named_children":{"object_type":{"constant":"transactions"}}},"field":{"constant":"amount"}}}`,
		},
		{
			Name:            "list_count",
			Category:        "aggregation",
			Description:     "Returns the number of items in a list.",
			ReturnType:      domainast.ValueTypeNumber,
			PositionalArity: intPtr(1),
			Example:         `{"function":"list_count","children":[{"function":"related_records","named_children":{"object_type":{"constant":"transactions"}}}]}`,
		},
		{
			Name:            "sum",
			Category:        "aggregation",
			Description:     "Sums a list of numeric values.",
			ReturnType:      domainast.ValueTypeNumber,
			PositionalArity: intPtr(1),
			Example:         `{"function":"sum","children":[{"function":"map_field","named_children":{"items":{"function":"related_records","named_children":{"object_type":{"constant":"transactions"}}},"field":{"constant":"amount"}}}]}`,
		},
		{
			Name:            "avg",
			Category:        "aggregation",
			Description:     "Returns the average of a list of numeric values.",
			ReturnType:      domainast.ValueTypeNumber,
			PositionalArity: intPtr(1),
			Example:         `{"function":"avg","children":[{"function":"map_field","named_children":{"items":{"function":"related_records","named_children":{"object_type":{"constant":"transactions"}}},"field":{"constant":"amount"}}}]}`,
		},
		{
			Name:            "min",
			Category:        "aggregation",
			Description:     "Returns the minimum value from a list of numeric values.",
			ReturnType:      domainast.ValueTypeNumber,
			PositionalArity: intPtr(1),
			Example:         `{"function":"min","children":[{"function":"map_field","named_children":{"items":{"function":"related_records","named_children":{"object_type":{"constant":"transactions"}}},"field":{"constant":"amount"}}}]}`,
		},
		{
			Name:            "max",
			Category:        "aggregation",
			Description:     "Returns the maximum value from a list of numeric values.",
			ReturnType:      domainast.ValueTypeNumber,
			PositionalArity: intPtr(1),
			Example:         `{"function":"max","children":[{"function":"map_field","named_children":{"items":{"function":"related_records","named_children":{"object_type":{"constant":"transactions"}}},"field":{"constant":"amount"}}}]}`,
		},
		{
			Name:              "group_count",
			Category:          "grouped aggregation",
			Description:       "Groups a list of object records by one field and returns counts per group key.",
			ReturnType:        domainast.ValueTypeObject,
			SupportsNamedArgs: true,
			Arguments: []FunctionArgument{
				{Name: "items", Kind: "list", Required: true, Description: "List of object records to group."},
				{Name: "field", Kind: "string", Required: true, Description: "Field used as the group key."},
			},
			Example: `{"function":"group_count","named_children":{"items":{"function":"related_records","named_children":{"object_type":{"constant":"transactions"}}},"field":{"constant":"status"}}}`,
		},
		{
			Name:              "group_sum",
			Category:          "grouped aggregation",
			Description:       "Groups a list of object records by one field and sums another numeric field per group key.",
			ReturnType:        domainast.ValueTypeObject,
			SupportsNamedArgs: true,
			Arguments: []FunctionArgument{
				{Name: "items", Kind: "list", Required: true, Description: "List of object records to group."},
				{Name: "group_field", Kind: "string", Required: true, Description: "Field used as the group key."},
				{Name: "value_field", Kind: "string", Required: true, Description: "Numeric field summed inside each group."},
			},
			Example: `{"function":"group_sum","named_children":{"items":{"function":"related_records","named_children":{"object_type":{"constant":"transactions"}}},"group_field":{"constant":"country"},"value_field":{"constant":"amount"}}}`,
		},
		{
			Name:              "object_get",
			Category:          "object access",
			Description:       "Reads one value from an object result such as group_count or group_sum output.",
			ReturnType:        domainast.ValueTypeUnknown,
			SupportsNamedArgs: true,
			Arguments: []FunctionArgument{
				{Name: "object", Kind: "object", Required: true, Description: "Object map to read from."},
				{Name: "key", Kind: "string", Required: true, Description: "Object key to return."},
			},
			Example: `{"function":"object_get","named_children":{"object":{"function":"group_count","named_children":{"items":{"function":"related_records","named_children":{"object_type":{"constant":"transactions"}}},"field":{"constant":"status"}}},"key":{"constant":"review"}}}`,
		},
	}
}

func booleanFunc(name, description string, arity *int, example string) FunctionDescriptor {
	return FunctionDescriptor{Name: name, Category: "boolean", Description: description, ReturnType: domainast.ValueTypeBool, PositionalArity: arity, Example: example}
}

func comparisonFunc(name, description string) FunctionDescriptor {
	return FunctionDescriptor{Name: name, Category: "comparison", Description: description, ReturnType: domainast.ValueTypeBool, PositionalArity: intPtr(2), Example: `{"function":"` + name + `","children":[{"function":"field_ref","named_children":{"field":{"constant":"amount"}}},{"constant":100}]}`}
}

func arithmeticFunc(name, description string) FunctionDescriptor {
	return FunctionDescriptor{Name: name, Category: "arithmetic", Description: description, ReturnType: domainast.ValueTypeNumber, PositionalArity: intPtr(2), Example: `{"function":"` + name + `","children":[{"function":"field_ref","named_children":{"field":{"constant":"amount"}}},{"constant":10}]}`}
}

func intPtr(value int) *int {
	return &value
}
