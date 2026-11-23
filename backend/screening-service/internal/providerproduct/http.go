package providerproduct

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/internalprovider"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/provideraccess"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourcedata"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type SearchInput struct {
	Profile   string                  `json:"profile"`
	Threshold int                     `json:"threshold"`
	Datasets  []sourcedata.DatasetRef `json:"dataset_selection"`
	Queries   []screening.SearchQuery `json:"queries"`
	Limit     int                     `json:"limit"`
}

func (in SearchInput) Request() (screening.SearchRequest, error) {
	if in.Profile == "" || in.Limit < 1 || in.Limit > 50 {
		return screening.SearchRequest{}, sourcedata.ErrInput
	}
	selection, e := json.Marshal(sourcedata.Selection{Version: 2, Profile: in.Profile, Threshold: in.Threshold, Datasets: in.Datasets})
	if e != nil {
		return screening.SearchRequest{}, e
	}
	if _, e = sourcedata.DecodeSelection(selection); e != nil {
		return screening.SearchRequest{}, e
	}
	return screening.SearchRequest{Provider: sourcedata.Provider, Queries: in.Queries, ProviderConfig: selection, LimitOverride: &in.Limit}, nil
}

type AccessControl interface {
	Authenticate(context.Context, string) (Access, error)
	Consume(context.Context, Access, string) error
}

type responseStatus struct {
	http.ResponseWriter
	status int
}

func (w *responseStatus) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
		w.ResponseWriter.WriteHeader(status)
	}
}
func (w *responseStatus) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(200)
	}
	return w.ResponseWriter.Write(body)
}

type Server struct {
	DB            *pgxpool.Pool
	Provider      ports.ScopedScreeningProvider
	Access        AccessControl
	Logger        *slog.Logger
	Timeout       time.Duration
	MaxConcurrent int
}

func (s Server) Handler() (http.Handler, error) {
	if s.Timeout < 10*time.Millisecond || s.Timeout > 5*time.Second || s.MaxConcurrent < 1 || s.MaxConcurrent > 256 || s.Access == nil || s.Provider == nil {
		return nil, errors.New("invalid provider HTTP limits or dependencies")
	}
	if s.Logger == nil {
		s.Logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	}
	mux := http.NewServeMux()
	slots := make(chan struct{}, s.MaxConcurrent)
	write := func(w http.ResponseWriter, status int, body any) {
		encoded, e := json.Marshal(body)
		if e != nil || len(encoded) >= 8*1024*1024 {
			status = 503
			encoded = []byte(`{"error":"provider_response_unavailable"}`)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if _, e := w.Write(append(encoded, '\n')); e != nil {
			s.Logger.Warn("provider response interrupted", "code", "response_write_failed")
		}
	}
	wrap := func(operation string, fn func(context.Context, provideraccess.Scope, http.ResponseWriter, *http.Request) error) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			output := &responseStatus{ResponseWriter: w}
			w = output
			started := time.Now()
			requestID := uuid.NewString()
			w.Header().Set("X-Request-ID", requestID)
			w.Header().Set("Cache-Control", "private, no-store")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			ctx, cancel := context.WithTimeout(r.Context(), s.Timeout)
			defer cancel()
			select {
			case slots <- struct{}{}:
				defer func() { <-slots }()
			default:
				w.Header().Set("Retry-After", "1")
				write(w, 429, map[string]string{"error": "provider_busy", "request_id": requestID})
				return
			}
			token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
			if !ok {
				write(w, 401, map[string]string{"error": "invalid_provider_credential", "request_id": requestID})
				return
			}
			access, e := s.Access.Authenticate(ctx, token)
			if e == nil {
				e = s.Access.Consume(ctx, access, operation)
			}
			if e == nil {
				e = fn(ctx, access.Scope, w, r)
			}
			status := 200
			if e != nil {
				var code string
				status, code = errorStatus(e)
				if status == 429 {
					w.Header().Set("Retry-After", "60")
				}
				write(w, status, map[string]string{"error": code, "request_id": requestID})
			}
			status = output.status
			s.Logger.Info("provider request", "request_id", requestID, "client_id", access.Scope.Client().String(), "operation", operation, "status", status, "duration_ms", time.Since(started).Milliseconds())
		}
	}
	mux.HandleFunc("POST /v1/search", wrap("search", func(ctx context.Context, scope provideraccess.Scope, w http.ResponseWriter, r *http.Request) error {
		var in SearchInput
		if e := decode(w, r, &in); e != nil {
			return e
		}
		request, e := in.Request()
		if e != nil {
			return e
		}
		result, e := s.Provider.Search(ctx, scope, request)
		if e != nil {
			return e
		}
		// Partial is explicit and must never be interpreted as a clean no-match.
		var body map[string]json.RawMessage
		if json.Unmarshal(result.RawResponse, &body) != nil || body == nil {
			return sourcedata.ErrEvidence
		}
		body["provider_reference"], _ = json.Marshal(result.ProviderReference)
		write(w, 200, body)
		return nil
	}))
	mux.HandleFunc("POST /v1/enrich", wrap("enrich", func(ctx context.Context, scope provideraccess.Scope, w http.ResponseWriter, r *http.Request) error {
		var in sourcedata.CandidateRef
		if e := decode(w, r, &in); e != nil {
			return e
		}
		result, e := s.Provider.Enrich(ctx, scope, in)
		if e != nil {
			return e
		}
		write(w, 200, result.RawPayload)
		return nil
	}))
	op := func(r *http.Request) (sourcedata.Operation, error) {
		values, err := url.ParseQuery(r.URL.RawQuery)
		if err != nil {
			return sourcedata.Operation{}, sourcedata.ErrInput
		}
		out := sourcedata.Operation{Provider: sourcedata.Provider, Cursor: values.Get("cursor")}
		if len(out.Cursor) > 100 {
			return out, sourcedata.ErrInput
		}
		if raw := values.Get("limit"); raw != "" {
			v, e := strconv.Atoi(raw)
			if e != nil || v < 1 || v > 500 {
				return out, sourcedata.ErrInput
			}
			out.Limit = v
		}
		for _, raw := range values["dataset"] {
			scope, id, ok := strings.Cut(raw, ":")
			ref := sourcedata.DatasetRef{Scope: scope, ID: id}
			if !ok || !ref.Valid() {
				return out, sourcedata.ErrInput
			}
			out.Selection = append(out.Selection, ref)
		}
		if len(out.Selection) > sourcedata.MaxSelectedDatasets {
			return out, sourcedata.ErrInput
		}
		for k := range values {
			if k != "dataset" && k != "cursor" && k != "limit" {
				return out, sourcedata.ErrInput
			}
			if k != "dataset" && len(values[k]) > 1 {
				return out, sourcedata.ErrInput
			}
		}
		return out, nil
	}
	mux.HandleFunc("GET /v1/datasets", wrap("catalog", func(ctx context.Context, scope provideraccess.Scope, w http.ResponseWriter, r *http.Request) error {
		in, e := op(r)
		if e != nil {
			return e
		}
		result, e := s.Provider.GetCatalog(ctx, scope, in)
		if e != nil {
			return e
		}
		write(w, 200, result.RawPayload)
		return nil
	}))
	mux.HandleFunc("GET /v1/freshness", wrap("freshness", func(ctx context.Context, scope provideraccess.Scope, w http.ResponseWriter, r *http.Request) error {
		in, e := op(r)
		if e != nil {
			return e
		}
		result, e := s.Provider.GetFreshness(ctx, scope, in)
		if e != nil {
			return e
		}
		write(w, 200, result.RawPayload)
		return nil
	}))
	mux.HandleFunc("GET /v1/deltas", wrap("delta", func(ctx context.Context, scope provideraccess.Scope, w http.ResponseWriter, r *http.Request) error {
		in, e := op(r)
		if e != nil {
			return e
		}
		result, e := s.Provider.GetDatasetDelta(ctx, scope, in)
		if e != nil {
			return e
		}
		var body map[string]json.RawMessage
		if json.Unmarshal(result.RawPayload, &body) != nil || body == nil {
			return sourcedata.ErrEvidence
		}
		body["next_cursor"], _ = json.Marshal(result.NextCursor)
		body["has_more"], _ = json.Marshal(result.HasMore)
		body["changed"], _ = json.Marshal(result.Changed)
		write(w, 200, body)
		return nil
	}))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		write(w, 200, map[string]string{"service": "screening-provider", "status": "alive"})
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		if s.DB == nil {
			write(w, 503, map[string]string{"status": "unavailable"})
			return
		}
		if _, e := Topology(ctx, s.DB); e != nil || !sourcedata.NormalizerSupported() {
			write(w, 503, map[string]string{"status": "unavailable"})
			return
		}
		write(w, 200, map[string]string{"status": "ready", "note": "Process/catalog readiness, not source rights or quality acceptance"})
	})
	registerDocumentation(mux)
	return mux, nil
}

func decode(w http.ResponseWriter, r *http.Request, out any) error {
	media, _, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if e != nil || media != "application/json" {
		return sourcedata.ErrInput
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, sourcedata.MaxReadRequestBytes))
	d.DisallowUnknownFields()
	if e = d.Decode(out); e != nil {
		return sourcedata.ErrInput
	}
	if e = d.Decode(new(any)); !errors.Is(e, io.EOF) {
		return sourcedata.ErrInput
	}
	return nil
}
func errorStatus(e error) (int, string) {
	switch {
	case errors.Is(e, ErrCredential):
		return 401, "invalid_provider_credential"
	case errors.Is(e, ErrOperation), errors.Is(e, provideraccess.ErrScope):
		return 403, "provider_access_denied"
	case errors.Is(e, ErrQuota):
		return 429, "provider_rate_limit"
	case errors.Is(e, sourcedata.ErrInput), errors.Is(e, sourcedata.ErrCursor):
		return 400, "invalid_provider_input"
	case errors.Is(e, sourcedata.ErrDataset):
		return 404, "dataset_unavailable"
	case errors.Is(e, sourcedata.ErrEvidence):
		return 410, "source_evidence_unavailable"
	case errors.Is(e, context.DeadlineExceeded), errors.Is(e, context.Canceled):
		return 504, "provider_deadline_exceeded"
	default:
		return 503, "provider_unavailable"
	}
}

var _ ports.ScopedScreeningProvider = internalprovider.Provider{}
