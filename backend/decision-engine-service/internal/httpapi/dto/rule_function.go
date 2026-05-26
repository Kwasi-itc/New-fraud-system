package dto

import (
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/runtime/ast_eval"
)

type RuleFunctionArgumentResponse struct {
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	Required    bool   `json:"required"`
	Description string `json:"description"`
}

type RuleFunctionResponse struct {
	Name              string                         `json:"name"`
	Category          string                         `json:"category"`
	Description       string                         `json:"description"`
	ReturnType        string                         `json:"return_type"`
	PositionalArity   *int                           `json:"positional_arity,omitempty"`
	SupportsNamedArgs bool                           `json:"supports_named_args"`
	Arguments         []RuleFunctionArgumentResponse `json:"arguments"`
	RequiresModel     bool                           `json:"requires_model"`
	RequiresDataRead  bool                           `json:"requires_data_read"`
	RequiresPlatform  bool                           `json:"requires_platform"`
	Example           string                         `json:"example"`
}

func AdaptRuleFunctionCatalog(items []ast_eval.FunctionDescriptor) []RuleFunctionResponse {
	out := make([]RuleFunctionResponse, len(items))
	for i, item := range items {
		args := make([]RuleFunctionArgumentResponse, len(item.Arguments))
		for j, arg := range item.Arguments {
			args[j] = RuleFunctionArgumentResponse{
				Name:        arg.Name,
				Kind:        arg.Kind,
				Required:    arg.Required,
				Description: arg.Description,
			}
		}
		out[i] = RuleFunctionResponse{
			Name:              item.Name,
			Category:          item.Category,
			Description:       item.Description,
			ReturnType:        string(item.ReturnType),
			PositionalArity:   item.PositionalArity,
			SupportsNamedArgs: item.SupportsNamedArgs,
			Arguments:         args,
			RequiresModel:     item.RequiresModel,
			RequiresDataRead:  item.RequiresDataRead,
			RequiresPlatform:  item.RequiresPlatform,
			Example:           item.Example,
		}
	}
	return out
}
