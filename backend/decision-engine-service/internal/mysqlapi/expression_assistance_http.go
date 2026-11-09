package mysqlapi

import (
	"database/sql"
	"encoding/json"
	"errors"
	ast "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/ast"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/expressionassist"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func (h *ExecutionHTTP) registerExpressionAssistance(v *gin.RouterGroup) {
	root := "/tenants/:tenantId/scenarios/:scenarioId"
	v.POST(root+"/ast-ai-description", h.handle("assist.describe"))
	v.POST(root+"/generate-ast", h.handle("assist.generate"))
	v.GET(root+"/iterations/:iterationId/rules/:ruleId/ai-description", h.handle("assist.rule"))
}
func (h *ExecutionHTTP) expressionAssistance(c *gin.Context, api *ExecutionEndpointScope, op string) (any, error) {
	if api.Authoring == nil || c.Request.URL.RawQuery != "" {
		return nil, mysql.ErrAdministrationInput
	}
	var node ast.Node
	parent := uuid.MustParse(c.Param("scenarioId"))
	ctx := c.Request.Context()
	if op == "assist.generate" {
		var req struct {
			Expression string `json:"expression"`
		}
		if err := decodeExecutionHTTP(c.Request.Body, 16<<10, &req); err != nil {
			return nil, err
		}
		var err error
		node, err = expressionassist.Compile(req.Expression)
		if err != nil {
			return nil, errors.Join(mysql.ErrAdministrationInput, err)
		}
	} else if op == "assist.describe" {
		var req struct {
			Formula *ast.Node `json:"formula"`
		}
		if err := decodeExecutionHTTP(c.Request.Body, min(h.maxBytes, 32<<10), &req); err != nil {
			return nil, err
		}
		if req.Formula == nil {
			return nil, errExecutionHTTPBody
		}
		node = *req.Formula
	} else {
		rules, err := api.Authoring.Rules(ctx, parent, uuid.MustParse(c.Param("iterationId")))
		if err != nil {
			return nil, err
		}
		found := false
		for _, rule := range rules {
			if rule.ID == c.Param("ruleId") {
				if err := json.Unmarshal(rule.Formula, &node); err != nil {
					return nil, err
				}
				found = true
				break
			}
		}
		if !found {
			return nil, sql.ErrNoRows
		}
	}
	return api.Authoring.AssistExpression(ctx, parent, node)
}
