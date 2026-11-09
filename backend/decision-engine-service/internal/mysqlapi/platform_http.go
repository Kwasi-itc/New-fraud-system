package mysqlapi

import (
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/httpapi/dto"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func (h *ExecutionHTTP) registerPlatform(v *gin.RouterGroup) {
	for _, r := range []struct{ method, path, op string }{
		{"GET", "custom-list-entries", "entries"}, {"POST", "custom-list-entries", "entry.create"}, {"GET", "record-tags", "tags"}, {"POST", "record-tags", "tag.create"},
		{"POST", "risk-snapshots", "risk.create"}, {"GET", "ip-flags", "flags"}, {"POST", "ip-flags", "flag.create"},
	} {
		v.Handle(r.method, "/tenants/:tenantId/platform/"+r.path, h.handle("platform."+r.op))
	}
}

// Require explicit filter keys, including when their exact value is empty.
// Never trim object identity, list names or IP text, or silently accept aliases.
func platformQuery(q url.Values, op string) error {
	keys := []string{}
	switch op {
	case "entries":
		keys = []string{"list_name"}
	case "tags":
		keys = []string{"object_type", "object_id"}
	case "flags":
		keys = []string{"ip_address"}
	}
	if len(q) != len(keys) {
		return mysql.ErrExecutionPage
	}
	for _, key := range keys {
		v := q[key]
		if len(v) != 1 || !utf8.ValidString(v[0]) || strings.ContainsRune(v[0], 0) {
			return mysql.ErrExecutionPage
		}
	}
	return nil
}
func (h *ExecutionHTTP) platform(c *gin.Context, a *ExecutionEndpointScope, op string, q url.Values) (any, int, error) {
	if e := platformQuery(q, op); e != nil {
		return nil, 0, e
	}
	ctx := c.Request.Context()
	if op == "entry.create" {
		if a.CustomLists == nil {
			return nil, 0, mysql.ErrOwnerContractUnavailable
		}
		var req struct {
			ListID string  `json:"list_id"`
			Value  *string `json:"value"`
		}
		if err := decodeExecutionHTTP(c.Request.Body, h.maxBytes, &req); err != nil {
			return nil, 0, err
		}
		id, err := uuid.Parse(req.ListID)
		if err != nil || id == uuid.Nil || req.Value == nil {
			return nil, 0, errExecutionHTTPBody
		}
		value, err := a.CustomLists.SaveEntry(ctx, id, uuid.Nil, *req.Value)
		return gin.H{"custom_list_entry": dto.AdaptCustomListEntry(value)}, 201, err
	}
	if op == "entries" {
		if a.CustomLists == nil {
			return nil, 0, mysql.ErrOwnerContractUnavailable
		}
		v, e := a.CustomLists.EntriesByName(ctx, q.Get("list_name"))
		return gin.H{"custom_list_entries": adaptHistory(v, dto.AdaptCustomListEntry)}, 200, e
	}
	if a.Platform == nil {
		return nil, 0, mysql.ErrOwnerContractUnavailable
	}
	switch op {
	case "tags":
		v, e := a.Platform.Tags(ctx, q.Get("object_type"), q.Get("object_id"))
		return gin.H{"record_tags": adaptHistory(v, dto.AdaptRecordTag)}, 200, e
	case "flags":
		v, e := a.Platform.Flags(ctx, q.Get("ip_address"))
		return gin.H{"ip_flags": adaptHistory(v, dto.AdaptIPFlag)}, 200, e
	case "tag.create":
		var req struct {
			ObjectType *string `json:"object_type"`
			ObjectID   *string `json:"object_id"`
			Tag        *string `json:"tag"`
		}
		if e := decodeExecutionHTTP(c.Request.Body, h.maxBytes, &req); e != nil {
			return nil, 0, e
		}
		if req.ObjectType == nil || req.ObjectID == nil || req.Tag == nil {
			return nil, 0, errExecutionHTTPBody
		}
		v, e := a.Platform.CreateTag(ctx, *req.ObjectType, *req.ObjectID, *req.Tag)
		return gin.H{"record_tag": dto.AdaptRecordTag(v)}, 201, e
	case "risk.create":
		var req struct {
			ObjectType *string `json:"object_type"`
			ObjectID   *string `json:"object_id"`
			RiskLevel  *string `json:"risk_level"`
		}
		if e := decodeExecutionHTTP(c.Request.Body, h.maxBytes, &req); e != nil {
			return nil, 0, e
		}
		if req.ObjectType == nil || req.ObjectID == nil || req.RiskLevel == nil {
			return nil, 0, errExecutionHTTPBody
		}
		v, e := a.Platform.CreateRisk(ctx, *req.ObjectType, *req.ObjectID, *req.RiskLevel)
		return gin.H{"risk_snapshot": dto.AdaptRiskSnapshot(v)}, 201, e
	case "flag.create":
		var req struct {
			IPAddress *string `json:"ip_address"`
			Flag      *string `json:"flag"`
		}
		if e := decodeExecutionHTTP(c.Request.Body, h.maxBytes, &req); e != nil {
			return nil, 0, e
		}
		if req.IPAddress == nil || req.Flag == nil {
			return nil, 0, errExecutionHTTPBody
		}
		v, e := a.Platform.CreateFlag(ctx, *req.IPAddress, *req.Flag)
		return gin.H{"ip_flag": dto.AdaptIPFlag(v)}, 201, e
	}
	return nil, 0, mysql.ErrOwnerContractUnavailable
}
