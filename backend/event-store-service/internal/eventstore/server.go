package eventstore

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

type Server struct {
	cfg        Config
	logger     *slog.Logger
	repository *Repository
}

func NewServer(cfg Config, logger *slog.Logger) (*Server, error) {
	if strings.TrimSpace(cfg.ClickHouseURL) == "" {
		return nil, fmt.Errorf("CLICKHOUSE_URL is required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	repository, err := NewRepository(cfg, logger)
	if err != nil {
		return nil, err
	}
	return &Server{cfg: cfg, logger: logger, repository: repository}, nil
}

func (s *Server) Initialize(ctx context.Context) error { return s.repository.Initialize(ctx) }

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /readyz", s.health)
	mux.HandleFunc("POST /internal/v1/events", s.writeEvent)
	mux.HandleFunc("POST /internal/v1/events/batch", s.writeBatch)
	mux.HandleFunc("POST /internal/v1/records/get", s.getRecord)
	mux.HandleFunc("POST /internal/v1/records/list", s.listRecords)
	mux.HandleFunc("POST /internal/v1/aggregates", s.aggregate)
	return mux
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.repository.Health(ctx); err != nil {
		writeError(w, http.StatusServiceUnavailable, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) writeEvent(w http.ResponseWriter, r *http.Request) {
	var request EventWriteRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := request.Table.validate(); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := request.Event.validate(request.Table); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := s.repository.Write(r.Context(), request.Table, request.Event); err != nil {
		writeError(w, http.StatusServiceUnavailable, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"stored": 1})
}

func (s *Server) writeBatch(w http.ResponseWriter, r *http.Request) {
	var batch EventBatch
	if err := decodeJSON(r, &batch); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if len(batch.Events) == 0 || len(batch.Events) > 500 {
		writeError(w, http.StatusBadRequest, fmt.Errorf("events must contain 1 to 500 records"))
		return
	}
	if err := batch.Table.validate(); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	for _, event := range batch.Events {
		if err := event.validate(batch.Table); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
	}
	if err := s.repository.WriteBatch(r.Context(), batch.Table, batch.Events); err != nil {
		writeError(w, http.StatusServiceUnavailable, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"stored": len(batch.Events)})
}

func (s *Server) getRecord(w http.ResponseWriter, r *http.Request) {
	var request RecordRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	record, err := s.repository.GetRecord(r.Context(), request)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"record": record})
}

func (s *Server) listRecords(w http.ResponseWriter, r *http.Request) {
	var request RecordRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	records, err := s.repository.ListRecords(r.Context(), request)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"records": records})
}

func (s *Server) aggregate(w http.ResponseWriter, r *http.Request) {
	var request AggregateRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := validateAggregateRequest(request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	value, err := s.repository.Aggregate(r.Context(), request)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"value": value})
}

func validateAggregateRequest(request AggregateRequest) error {
	return ValidateAggregateRequest(request)
}

// A lower bound inside OR or NOT does not bound the entire query; only an AND
// path (or a single predicate) can establish the required event-time window.
func hasEventTimeLowerBound(filter *AggregateFilter, field string, positive bool) bool {
	if filter == nil || !positive {
		return false
	}
	kind := strings.ToLower(strings.TrimSpace(filter.Kind))
	if kind == "predicate" {
		return filter.Field == field && (strings.EqualFold(filter.Op, "gt") || strings.EqualFold(filter.Op, "gte"))
	}
	op := strings.ToLower(strings.TrimSpace(filter.Operator))
	if op == "" {
		op = "and"
	}
	if op != "and" {
		return false
	}
	for i := range filter.Children {
		if hasEventTimeLowerBound(&filter.Children[i], field, true) {
			return true
		}
	}
	return false
}

func decodeJSON(r *http.Request, target any) error {
	decoder := json.NewDecoder(io.LimitReader(r.Body, 8<<20))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"message": err.Error()}})
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
