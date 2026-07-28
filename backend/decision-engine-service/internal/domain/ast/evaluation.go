package ast

type EvaluationNode struct {
	Function      string                    `json:"function,omitempty"`
	Constant      any                       `json:"constant,omitempty"`
	ReturnValue   any                       `json:"return_value,omitempty"`
	Children      []EvaluationNode          `json:"children,omitempty"`
	NamedChildren map[string]EvaluationNode `json:"named_children,omitempty"`
	Skipped       bool                      `json:"skipped,omitempty"`
}
