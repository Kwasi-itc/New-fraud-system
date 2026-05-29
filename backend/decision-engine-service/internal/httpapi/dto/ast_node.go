package dto

import domainast "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/ast"

type NodeResponse struct {
	Name          string                  `json:"name,omitempty"`
	Constant      any                     `json:"constant,omitempty"`
	Children      []NodeResponse          `json:"children,omitempty"`
	NamedChildren map[string]NodeResponse `json:"named_children,omitempty"`
}

func AdaptNode(node domainast.Node) NodeResponse {
	children := make([]NodeResponse, len(node.Children))
	for i, child := range node.Children {
		children[i] = AdaptNode(child)
	}

	namedChildren := make(map[string]NodeResponse, len(node.NamedChildren))
	for key, child := range node.NamedChildren {
		namedChildren[key] = AdaptNode(child)
	}

	return NodeResponse{
		Name:          node.Function,
		Constant:      node.Constant,
		Children:      children,
		NamedChildren: namedChildren,
	}
}
