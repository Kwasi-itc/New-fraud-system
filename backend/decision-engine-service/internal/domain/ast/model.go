package ast

type Node struct {
	Function      string          `json:"function"`
	Constant      any             `json:"constant,omitempty"`
	Children      []Node          `json:"children,omitempty"`
	NamedChildren map[string]Node `json:"named_children,omitempty"`
}

type ValueType string

const (
	ValueTypeUnknown   ValueType = "unknown"
	ValueTypeBool      ValueType = "bool"
	ValueTypeString    ValueType = "string"
	ValueTypeNumber    ValueType = "number"
	ValueTypeTimestamp ValueType = "timestamp"
	ValueTypeNull      ValueType = "null"
	ValueTypeList      ValueType = "list"
)
