package mysqlapi

import (
	"encoding/base64"
	"encoding/json"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/httpapi/dto"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func (h *ExecutionHTTP) publicationPreparation(c *gin.Context, api *ExecutionEndpointScope, parent uuid.UUID, start bool) (any, error) {
	if api.Publication == nil {
		return nil, mysql.ErrOwnerContractUnavailable
	}
	var req struct {
		IterationID string `json:"iteration_id"`
	}
	if start {
		if err := decodeExecutionHTTP(c.Request.Body, h.maxBytes, &req); err != nil {
			return nil, err
		}
	} else {
		q := c.Request.URL.Query()
		if len(q) != 1 || len(q["iteration_id"]) != 1 {
			return nil, errExecutionHTTPBody
		}
		req.IterationID = q.Get("iteration_id")
	}
	id, err := uuid.Parse(req.IterationID)
	if err != nil || id == uuid.Nil {
		return nil, errExecutionHTTPBody
	}
	result, err := api.Publication.Preparation(c.Request.Context(), parent, id, start)
	if err != nil {
		return nil, err
	}
	return gin.H{"preparation": dto.AdaptPublicationPreparationStatus(result)}, nil
}

// Publication actions preserve the source request/response format. Event IDs
// are server allocated, not caller authority or an idempotency promise. Ambiguous
// commits use the shared explicit unknown-outcome response; never auto-retry.
func (h *ExecutionHTTP) publication(c *gin.Context, api *ExecutionEndpointScope, tenant, parent uuid.UUID, history bool) (any, error) {
	if api.Publication == nil {
		return nil, mysql.ErrOwnerContractUnavailable
	}
	ctx := c.Request.Context()
	var events []scenario.Publication
	var err error
	next := ""
	paged := history && c.Request.URL.RawQuery != ""
	if history {
		if paged {
			filter, e := publicationPageFilter(c.Request.URL.Query())
			if e != nil {
				return nil, e
			}
			page, e := api.Publication.HistoryPage(ctx, tenant, parent, filter)
			if e != nil {
				return nil, e
			}
			events = page.Items
			if page.Next != nil {
				raw, e := json.Marshal(page.Next)
				if e != nil {
					return nil, e
				}
				next = base64.RawURLEncoding.EncodeToString(raw)
			}
		} else {
			events, err = api.Publication.History(ctx, tenant, parent)
		}
	} else {
		var req dto.PublicationActionRequest
		if err := decodeExecutionHTTP(c.Request.Body, h.maxBytes, &req); err != nil {
			return nil, err
		}
		iteration, err := uuid.Parse(req.IterationID)
		if err != nil || iteration == uuid.Nil || (req.Action != "publish" && req.Action != "unpublish") {
			return nil, errExecutionHTTPBody
		}
		event, err := uuid.NewRandom()
		if err != nil {
			return nil, err
		}
		input := mysql.PublicationInput{ScenarioID: parent, IterationID: iteration, EventID: event}
		if req.Action == "publish" {
			input.ReplacedEventID, err = uuid.NewRandom()
			if err != nil {
				return nil, err
			}
			events, err = api.Publication.Publish(ctx, input)
		} else {
			events, err = api.Publication.Unpublish(ctx, input)
		}
		if err != nil {
			return nil, err
		}
	}
	if err != nil {
		return nil, err
	}
	out := make([]dto.PublicationResponse, len(events))
	for i, event := range events {
		out[i] = dto.AdaptPublication(event)
	}
	if paged {
		return gin.H{"publications": out, "next_cursor": next}, nil
	}
	return gin.H{"publications": out}, nil
}

func (h *ExecutionHTTP) deactivate(c *gin.Context, api *ExecutionEndpointScope) (any, error) {
	if api.Publication == nil {
		return nil, mysql.ErrOwnerContractUnavailable
	}
	if err := h.emptyAuthoringBody(c); err != nil {
		return nil, err
	}
	event, err := uuid.NewRandom()
	if err != nil {
		return nil, err
	}
	values, err := api.Publication.Unpublish(c.Request.Context(), mysql.PublicationInput{ScenarioID: uuid.MustParse(c.Param("scenarioId")), IterationID: uuid.MustParse(c.Param("iterationId")), EventID: event})
	if err != nil {
		return nil, err
	}
	out := make([]dto.PublicationResponse, len(values))
	for i, v := range values {
		out[i] = dto.AdaptPublication(v)
	}
	return gin.H{"publications": out}, nil
}
