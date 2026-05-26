package ast

import "encoding/json"

type Node struct {
	Function      string          `json:"function"`
	Constant      any             `json:"constant,omitempty"`
	Children      []Node          `json:"children,omitempty"`
	NamedChildren map[string]Node `json:"named_children,omitempty"`
}

func (n *Node) UnmarshalJSON(data []byte) error {
	type alias struct {
		Function      string          `json:"function"`
		Name          string          `json:"name"`
		Constant      any             `json:"constant"`
		Children      []Node          `json:"children"`
		NamedChildren map[string]Node `json:"named_children"`
	}
	var raw alias
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	n.Function = raw.Function
	if n.Function == "" {
		n.Function = raw.Name
	}
	n.Constant = raw.Constant
	n.Children = raw.Children
	n.NamedChildren = raw.NamedChildren
	return nil
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
	ValueTypeObject    ValueType = "object"
)
