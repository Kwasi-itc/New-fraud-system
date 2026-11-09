package mysqlapi

import (
	"bytes"
	"encoding/csv"
	"io"
	"mime"
	"mime/multipart"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/platform"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/httpapi/dto"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
	"github.com/gin-gonic/gin"
	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
)

func (h *ExecutionHTTP) registerCustomLists(v *gin.RouterGroup) {
	p := "/tenants/:tenantId/platform/custom-lists"
	e := p + "/:listId/entries"
	for _, r := range []struct{ method, path, op string }{{"GET", p, "list"}, {"POST", p, "create"}, {"GET", p + "/:listId", "get"}, {"PUT", p + "/:listId", "update"}, {"DELETE", p + "/:listId", "delete"}, {"GET", e, "entries"}, {"POST", e, "entry.create"}, {"POST", e + "/import", "import"}, {"PUT", e + "/:entryId", "entry.update"}, {"DELETE", e + "/:entryId", "entry.delete"}} {
		v.Handle(r.method, r.path, h.handle("lists."+r.op))
	}
}

// Parse a single bounded CSV file without multipart temporary files. Parse the
// complete upload before entering a mutation, including its closing boundary.
func customListCSV(body io.Reader, contentType string, maxBytes int64, maxRows int) ([]string, error) {
	raw, e := io.ReadAll(io.LimitReader(body, maxBytes+1))
	if e != nil {
		return nil, errExecutionHTTPBody
	}
	if int64(len(raw)) > maxBytes {
		return nil, errExecutionHTTPSize
	}
	kind, params, e := mime.ParseMediaType(contentType)
	if e != nil || kind != "multipart/form-data" || params["boundary"] == "" {
		return nil, errExecutionHTTPBody
	}
	parts := multipart.NewReader(bytes.NewReader(raw), params["boundary"])
	part, e := parts.NextPart()
	if e != nil {
		return nil, errExecutionHTTPBody
	}
	defer part.Close()
	if part.FormName() != "file" || part.FileName() == "" {
		return nil, errExecutionHTTPBody
	}
	reader := csv.NewReader(part)
	reader.FieldsPerRecord = -1
	values := []string{}
	records := 0
	for {
		record, e := reader.Read()
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, errExecutionHTTPBody
		}
		records++
		if records > maxRows+1 {
			return nil, mysql.ErrProjectionLimit
		}
		if len(record) == 0 {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(record[0], "\uFEFF"))
		if records == 1 && (strings.EqualFold(value, "value") || strings.EqualFold(value, "values")) {
			continue
		}
		if len(values) >= maxRows {
			return nil, mysql.ErrProjectionLimit
		}
		if value != "" {
			values = append(values, value)
		}
	}
	if _, e := parts.NextPart(); e != io.EOF {
		return nil, errExecutionHTTPBody
	}
	return values, nil
}
func (h *ExecutionHTTP) customLists(c *gin.Context, s *mysql.CustomListAdmin, op string, rowBudget int) (out any, status int, err error) {
	defer func() {
		if e, ok := err.(*mysqldriver.MySQLError); ok && (e.Number == 1062 || e.Number == 1451 || e.Number == 1452) {
			err = mysql.ErrOwnerRelationshipConflict
		}
	}()
	if s == nil {
		return nil, 0, mysql.ErrOwnerContractUnavailable
	}
	ctx := c.Request.Context()
	param := func(name string) uuid.UUID {
		if c.Param(name) == "" {
			return uuid.Nil
		}
		return uuid.MustParse(c.Param(name))
	}
	list, entry := param("listId"), param("entryId")
	switch op {
	case "list":
		v, e := s.Lists(ctx)
		return gin.H{"custom_lists": adaptHistory(v, dto.AdaptCustomList)}, 200, e
	case "get":
		v, e := s.Get(ctx, list)
		return gin.H{"custom_list": dto.AdaptCustomList(v)}, 200, e
	case "create", "update":
		var req dto.CreateCustomListRequest
		if e := decodeExecutionHTTP(c.Request.Body, h.maxBytes, &req); e != nil {
			return nil, 0, e
		}
		v, e := s.Save(ctx, list, platform.CustomList{Name: req.Name, Description: req.Description, Kind: req.Kind})
		status = 200
		if op == "create" {
			status = 201
		}
		return gin.H{"custom_list": dto.AdaptCustomList(v)}, status, e
	case "delete":
		if e := h.emptyAuthoringBody(c); e != nil {
			return nil, 0, e
		}
		return nil, 204, s.Delete(ctx, list)
	case "entries":
		v, e := s.Entries(ctx, list)
		return gin.H{"custom_list_entries": adaptHistory(v, dto.AdaptCustomListEntry)}, 200, e
	case "entry.create", "entry.update":
		var req struct {
			Value *string `json:"value"`
		}
		if e := decodeExecutionHTTP(c.Request.Body, h.maxBytes, &req); e != nil {
			return nil, 0, e
		}
		if req.Value == nil {
			return nil, 0, errExecutionHTTPBody
		}
		v, e := s.SaveEntry(ctx, list, entry, *req.Value)
		status = 200
		if op == "entry.create" {
			status = 201
		}
		return gin.H{"custom_list_entry": dto.AdaptCustomListEntry(v)}, status, e
	case "entry.delete":
		if e := h.emptyAuthoringBody(c); e != nil {
			return nil, 0, e
		}
		return nil, 204, s.DeleteEntry(ctx, list, entry)
	case "import":
		values, e := customListCSV(c.Request.Body, c.GetHeader("Content-Type"), h.maxBytes, rowBudget)
		if e != nil {
			return nil, 0, e
		}
		n, e := s.Import(ctx, list, values)
		return dto.ImportCustomListEntriesResponse{ImportedCount: n}, 200, e
	default:
		return nil, 0, mysql.ErrAdministrationInput
	}
}
