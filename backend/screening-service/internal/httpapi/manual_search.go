package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"sort"
	"strings"

	identity "github.com/Kwasi-itc/New-fraud-system/backend/investigator"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/clients/provider"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type manualService interface {
	ListManualSearches(context.Context, string, string, screening.ManualHistoryFilter) (screening.ManualHistoryPage, error)
	CreateManualSearch(context.Context, string, string, service.ManualSearchInput) (screening.Screening, error)
	GetManualSearch(context.Context, string, string, string) (screening.Details, error)
}
type manualAuthorizer func(*http.Request) (identity.Principal, error)
type manualIdentityDB interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func registerManualSearch(router *gin.Engine, db *pgxpool.Pool, verifier *identity.Verifier, svc manualService, queueReady bool, capabilities provider.ManualCapabilities) {
	registerManualRoutes(router, manualAuthorization(db, verifier, queueReady && db != nil), svc, capabilities)
}

func manualAuthorization(db manualIdentityDB, verifier *identity.Verifier, ready bool) manualAuthorizer {
	return func(r *http.Request) (identity.Principal, error) {
		if db == nil || verifier == nil || !ready {
			return identity.Principal{}, &identity.Error{Code: 503, Message: "Screening is not configured."}
		}
		raw, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok {
			return identity.Principal{}, &identity.Error{Code: 401, Message: "Sign in to screen entities."}
		}
		if _, err := verifier.VerifyPlatformSession(raw); err != nil {
			return identity.Principal{}, &identity.Error{Code: 401, Message: "A valid platform session is required."}
		}
		p, err := verifier.Verify(raw)
		if err != nil {
			return p, &identity.Error{Code: 403, Message: "An investigator role is required."}
		}
		return p, identity.CheckIdentity(r.Context(), db, p, false)
	}
}

func registerManualRoutes(router *gin.Engine, authorize manualAuthorizer, svc manualService, catalog ...provider.ManualCapabilities) {
	handle := func(c *gin.Context) {
		c.Header("Cache-Control", "private, no-store")
		p, err := authorize(c.Request)
		if err != nil {
			manualError(c, err)
			return
		}
		if c.Param("id") == "capabilities" && c.Request.Method == http.MethodGet {
			if len(catalog) == 0 {
				c.JSON(503, gin.H{"error": "Provider capabilities unavailable."})
				return
			}
			c.JSON(200, catalog[0])
			return
		}
		if c.Request.Method == http.MethodPost {
			if c.ContentType() != "application/json" {
				c.JSON(415, gin.H{"error": "Expected application/json."})
				return
			}
			var in service.ManualSearchInput
			decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 4096))
			decoder.DisallowUnknownFields()
			if decoder.Decode(&in) != nil || decoder.Decode(new(any)) != io.EOF {
				c.JSON(400, gin.H{"error": "Invalid search request."})
				return
			}
			if len(catalog) > 0 {
				cap := catalog[0]
				if !cap.Configured {
					c.JSON(503, gin.H{"error": "Screening provider is not configured."})
					return
				}
				if !manualCapabilityAllows(cap, in) {
					c.JSON(400, gin.H{"error": "Search fields or matching settings are unsupported by this provider connection."})
					return
				}
			}
			item, err := svc.CreateManualSearch(c.Request.Context(), p.TenantID.String(), p.Subject, in)
			if err != nil {
				manualError(c, err)
				return
			}
			c.JSON(202, gin.H{"id": item.ID, "status": item.Status})
			return
		}
		if c.Param("id") == "" {
			f, err := service.ParseManualHistoryFilter(c.Request.URL.Query())
			if err != nil {
				c.JSON(400, gin.H{"error": "Invalid history filters or cursor."})
				return
			}
			page, err := svc.ListManualSearches(c.Request.Context(), p.TenantID.String(), p.Subject, f)
			if err != nil {
				manualError(c, err)
				return
			}
			c.JSON(200, page)
			return
		}
		id, err := uuid.Parse(c.Param("id"))
		if err != nil || id == uuid.Nil {
			c.JSON(400, gin.H{"error": "Invalid search identifier."})
			return
		}
		details, err := svc.GetManualSearch(c.Request.Context(), p.TenantID.String(), p.Subject, id.String())
		if err != nil {
			manualError(c, err)
			return
		}
		c.JSON(200, manualView(details))
	}
	router.POST("/v1/manual-screenings", handle)
	router.GET("/v1/manual-screenings", handle)
	router.GET("/v1/manual-screenings/:id", handle)
}

func manualCapabilityAllows(cap provider.ManualCapabilities, in service.ManualSearchInput) bool {
	if !slices.Contains(cap.EntityTypes, in.EntityType) || !slices.Contains(cap.Fields[in.EntityType], "name") ||
		in.Threshold < cap.ThresholdMin || in.Threshold > cap.ThresholdMax ||
		in.Limit < 1 || in.Limit > cap.LimitMax || (!cap.DatasetSelection && len(in.Datasets) > 0) {
		return false
	}
	for field, value := range map[string]string{
		"birth_date": in.BirthDate, "nationality": in.Nationality, "country": in.Country,
		"registration_number": in.RegistrationNumber, "address": in.Address,
	} {
		if strings.TrimSpace(value) != "" && !slices.Contains(cap.Fields[in.EntityType], field) {
			return false
		}
	}
	return true
}

func manualError(c *gin.Context, err error) {
	var auth *identity.Error
	switch {
	case errors.As(err, &auth):
		c.JSON(auth.Code, gin.H{"error": auth.Message})
	case errors.Is(err, service.ErrManualInput):
		c.JSON(400, gin.H{"error": "Invalid search settings. Check the name, entity-specific fields, ISO country codes, birth date, dataset IDs, threshold and result limit."})
	case errors.Is(err, service.ErrManualConflict):
		c.JSON(409, gin.H{"error": "This operation belongs to another search. Start a new search."})
	case errors.Is(err, pgx.ErrNoRows):
		c.JSON(404, gin.H{"error": "Search not found."})
	default:
		c.JSON(503, gin.H{"error": "Search is temporarily unavailable. Retry the same request."})
	}
}

// Deliberate projection: never expose provider errors, credentials, raw requests,
// case links or arbitrary provider payloads to the browser.
func manualView(details screening.Details) gin.H {
	matches := append([]screening.Match(nil), details.Matches...)
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Score == matches[j].Score {
			return matches[i].EntityID < matches[j].EntityID
		}
		return matches[i].Score > matches[j].Score
	})
	items := make([]gin.H, 0, len(matches))
	for _, match := range matches {
		var payload struct {
			Schema     string              `json:"schema"`
			Datasets   []string            `json:"datasets"`
			Properties map[string][]string `json:"properties"`
		}
		_ = json.Unmarshal(match.Payload, &payload)
		properties := map[string][]string{}
		for _, key := range []string{"alias", "country", "nationality", "birthDate", "address", "topics"} {
			if values := payload.Properties[key]; len(values) > 0 {
				properties[key] = values
			}
		}
		items = append(items, gin.H{"id": match.ID, "entity_id": match.EntityID, "name": match.Name, "status": match.Status, "score": match.Score, "schema": payload.Schema, "datasets": payload.Datasets, "properties": properties})
	}
	return gin.H{"id": details.Screening.ID, "provider": details.Screening.Provider, "status": details.Screening.Status, "partial": details.Screening.Partial, "created_at": details.Screening.CreatedAt, "matches": items, "query": service.ManualSearchSettings(details.Screening.RequestJSON)}
}
