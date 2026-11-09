package mysqlexecution

import (
	"context"
	"errors"
	ast "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/ast"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/expressionassist"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/runtime/ast_eval"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/service"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
	"github.com/google/uuid"
)

type ExpressionAssistance struct {
	Backend       string   `json:"backend"`
	UsesAI        bool     `json:"uses_ai"`
	Formula       ast.Node `json:"formula"`
	Description   string   `json:"description"`
	ModelRevision string   `json:"model_revision"`
}

func (a *Authoring) AssistExpression(ctx context.Context, parent uuid.UUID, node ast.Node) (ExpressionAssistance, error) {
	out := ExpressionAssistance{Backend: "local-expression-assistant", Formula: node}
	description, err := expressionassist.Describe(node)
	if err != nil {
		return out, errors.Join(mysql.ErrAdministrationInput, err)
	}
	before, err := a.Scenario(ctx, parent)
	if err != nil {
		return out, err
	}
	model, err := a.reader.ResolveExecutionModel(ctx)
	if err != nil {
		return out, err
	}
	model, err = service.CloneExecutionModel(model, a.limits.MaxBytes)
	if err != nil {
		return out, err
	}
	if err := a.reader.ValidateExecutionObjectModel(ctx, model, before.TriggerObjectType); err != nil {
		return out, err
	}
	kind, issues := ast_eval.ValidateNode(node, model, before.TriggerObjectType)
	if kind != ast.ValueTypeBool || len(issues) != 0 {
		return out, mysql.ErrAdministrationInput
	}
	after, err := a.Scenario(ctx, parent)
	if err != nil {
		return out, err
	}
	if after.TriggerObjectType != before.TriggerObjectType {
		return out, mysql.ErrExecutionBindingChanged
	}
	out.Description, out.ModelRevision = description, model.RevisionID
	return out, nil
}
