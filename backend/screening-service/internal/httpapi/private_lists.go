package httpapi

import (
	"encoding/json"
	"errors"
	platformauth "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
	"io"
	"net/http"
	"regexp"
	"strings"

	identity "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/privatelists"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourcedata"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

var privateListID = regexp.MustCompile(`^private-[a-f0-9]{32}$`)

// This route group has no service-token/operator fallback. The tenant and actor
// come exclusively from a signed platform session and current database roles.
func registerPrivateLists(r *platformauth.Router, db, writer *pgxpool.Pool, verifier *identity.Verifier, policy privatelists.Policy) {
	svc := privatelists.Service{DB: writer, Policy: policy}
	handle := func(c *gin.Context) {
		c.Header("Cache-Control", "private, no-store")
		if db == nil || verifier == nil {
			c.JSON(503, gin.H{"error": "Private lists are not configured."})
			return
		}
		raw, ok := strings.CutPrefix(c.GetHeader("Authorization"), "Bearer ")
		if !ok {
			c.JSON(401, gin.H{"error": "Sign in to manage private lists."})
			return
		}
		p, err := verifier.VerifyPlatformSession(raw)
		if err != nil {
			c.JSON(401, gin.H{"error": "A valid platform session is required."})
			return
		}
		if !p.Can("private-lists:read") {
			privateListError(c, identity.Forbidden())
			return
		}
		if c.Param("listId") == "capabilities" && c.Request.Method == http.MethodGet {
			can := func(capability string) bool {
				return policy.Configured() && writer != nil && p.Can(capability)
			}
			c.JSON(200, gin.H{"configured": policy.Configured() && writer != nil, "can_import": can(policy.ImportCapability), "can_approve": can(policy.ApproveCapability), "actor_id": p.Subject, "different_reviewer_required": true, "max_bytes": privatelists.MaxCSVBytes, "max_records": privatelists.MaxCSVRecords})
			return
		}
		if writer == nil || !policy.Configured() {
			privateListError(c, privatelists.ErrPolicy)
			return
		}
		list := c.Param("listId")
		if list != "" && !privateListID.MatchString(list) {
			privateListError(c, privatelists.ErrInput)
			return
		}
		read := func(out any) bool {
			if c.ContentType() != "application/json" {
				c.JSON(415, gin.H{"error": "Expected application/json."})
				return false
			}
			dec := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, privatelists.MaxRequestBytes))
			dec.DisallowUnknownFields()
			if dec.Decode(out) != nil || dec.Decode(new(any)) != io.EOF {
				privateListError(c, privatelists.ErrInput)
				return false
			}
			return true
		}
		var out any
		switch c.FullPath() {
		case "/v1/private-lists":
			if c.Request.Method == http.MethodGet {
				out, err = svc.List(c.Request.Context(), p, c.Query("after"))
			} else {
				var in privatelists.CreateInput
				if !read(&in) {
					return
				}
				out, err = svc.Create(c.Request.Context(), p, in)
				if err == nil {
					out = gin.H{"id": out}
				}
			}
		case "/v1/private-lists/:listId":
			var before *uuid.UUID
			if cursor := c.Query("before"); cursor != "" {
				id, e := uuid.Parse(cursor)
				if e != nil {
					privateListError(c, privatelists.ErrInput)
					return
				}
				before = &id
			}
			out, err = svc.Detail(c.Request.Context(), p, list, before)
		case "/v1/private-lists/:listId/imports":
			var in privatelists.ImportInput
			if !read(&in) {
				return
			}
			out, err = svc.Submit(c.Request.Context(), p, list, in)
		case "/v1/private-lists/:listId/recover":
			var in struct{}
			if !read(&in) {
				return
			}
			var n int64
			n, err = svc.Recover(c.Request.Context(), p, list)
			out = gin.H{"recovered": n}
		default:
			id, e := uuid.Parse(c.Param("importId"))
			if e != nil || id == uuid.Nil {
				privateListError(c, privatelists.ErrInput)
				return
			}
			switch {
			case strings.HasSuffix(c.FullPath(), "/artifact"):
				var body []byte
				var digest string
				body, digest, err = svc.Artifact(c.Request.Context(), p, list, id)
				if err == nil {
					c.Header("Content-Disposition", `attachment; filename="original-upload.txt"`)
					c.Header("X-Content-Type-Options", "nosniff")
					c.Header("X-Artifact-SHA256", digest)
					c.Data(200, "text/plain; charset=utf-8", body)
					return
				}
			case strings.HasSuffix(c.FullPath(), "/diff"):
				out, err = svc.Diff(c.Request.Context(), p, list, id, c.Query("after"))
			case strings.HasSuffix(c.FullPath(), "/approve"):
				var in struct{}
				if !read(&in) {
					return
				}
				out, err = svc.Approve(c.Request.Context(), p, list, id)
			case strings.HasSuffix(c.FullPath(), "/reject"):
				var in struct{}
				if !read(&in) {
					return
				}
				err = svc.Reject(c.Request.Context(), p, list, id)
				out = gin.H{"status": "rejected"}
			default:
				out, err = svc.GetImport(c.Request.Context(), p, list, id)
			}
		}
		if err != nil {
			privateListError(c, err)
			return
		}
		c.JSON(200, out)
	}
	r.GET("/v1/private-lists", platformauth.Policy{Capability: "private-lists:read", PrincipalType: platformauth.PrincipalUser}, handle)
	r.POST("/v1/private-lists", platformauth.Policy{Capability: "private-lists:import", PrincipalType: platformauth.PrincipalUser}, handle)
	r.GET("/v1/private-lists/:listId", platformauth.Policy{Capability: "private-lists:read", PrincipalType: platformauth.PrincipalUser}, handle)
	r.POST("/v1/private-lists/:listId/imports", platformauth.Policy{Capability: "private-lists:import", PrincipalType: platformauth.PrincipalUser}, handle)
	r.POST("/v1/private-lists/:listId/recover", platformauth.Policy{Capability: "private-lists:import", PrincipalType: platformauth.PrincipalUser}, handle)
	r.GET("/v1/private-lists/:listId/imports/:importId", platformauth.Policy{Capability: "private-lists:read", PrincipalType: platformauth.PrincipalUser}, handle)
	for _, action := range []string{"artifact", "diff"} {
		r.GET("/v1/private-lists/:listId/imports/:importId/"+action, platformauth.Policy{Capability: "private-lists:read", PrincipalType: platformauth.PrincipalUser}, handle)
	}
	for _, action := range []string{"approve", "reject"} {
		r.POST("/v1/private-lists/:listId/imports/:importId/"+action, platformauth.Policy{Capability: "private-lists:approve", PrincipalType: platformauth.PrincipalUser}, handle)
	}
}

func privateListError(c *gin.Context, err error) {
	status, message := 503, "Private lists are temporarily unavailable."
	switch {
	case errors.Is(err, privatelists.ErrInput), errors.Is(err, sourcedata.ErrInput):
		status, message = 400, "Invalid private-list input. Check the CSV columns, row values and unique record IDs."
	case errors.Is(err, privatelists.ErrNotFound):
		status, message = 404, "Private list or import not found."
	case errors.Is(err, privatelists.ErrConflict), errors.Is(err, sourcedata.ErrConflict):
		status, message = 409, "The list changed or this operation cannot be repeated with different data. Submit a new import."
	case errors.Is(err, privatelists.ErrPolicy):
		message = "Private-list permissions have not been configured."
	case errors.Is(err, privatelists.ErrExpired):
		status, message = 410, "The retained upload is no longer available."
	default:
		var problem *identity.Error
		if errors.As(err, &problem) {
			status, message = identity.HTTPError(err)
		}
	}
	c.JSON(status, gin.H{"error": message})
}
