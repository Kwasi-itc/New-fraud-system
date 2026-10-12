package platform

import (
	"encoding/json"
	"github.com/google/uuid"
	"net/http"
	"slices"
)

func (s Server) auditList(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Session string    `json:"session"`
		After   uuid.UUID `json:"after"`
	}
	if !decode(w, r, &in) {
		return
	}
	c, err := s.Store.Resolve(r.Context(), in.Session)
	if err != nil {
		s.fail(w, err)
		return
	}
	if !slices.Contains(c.Roles, "tenant_admin") {
		s.fail(w, ErrDenied)
		return
	}
	rows, err := s.Store.DB.Query(r.Context(), `SELECT id,actor_id,action,target_id,created_at::text,details FROM identity.audit_events WHERE tenant_id=$1 AND id>$2 ORDER BY id LIMIT 101`, c.TenantID, in.After)
	if err != nil {
		s.fail(w, err)
		return
	}
	defer rows.Close()
	type event struct {
		ID      uuid.UUID       `json:"id"`
		Actor   string          `json:"actor"`
		Action  string          `json:"action"`
		Target  uuid.UUID       `json:"target"`
		At      string          `json:"at"`
		Details json.RawMessage `json:"details"`
	}
	items := []event{}
	for rows.Next() {
		var e event
		if err = rows.Scan(&e.ID, &e.Actor, &e.Action, &e.Target, &e.At, &e.Details); err != nil {
			s.fail(w, err)
			return
		}
		items = append(items, e)
	}
	if err = rows.Err(); err != nil {
		s.fail(w, err)
		return
	}
	next := ""
	if len(items) > 100 {
		items = items[:100]
		next = items[99].ID.String()
	}
	respond(w, map[string]any{"items": items, "next_after": next})
}

func (s Server) invitationStart(w http.ResponseWriter, r *http.Request) {
	if s.Invitations == nil {
		s.fail(w, ErrDenied)
		return
	}
	var in struct {
		Token string `json:"token"`
	}
	if !decode(w, r, &in) {
		return
	}
	l, err := s.Invitations.Begin(r.Context(), in.Token)
	if err != nil {
		s.fail(w, err)
		return
	}
	u, err := s.Provider.AuthorizationURL(l.State, l.Nonce, l.Verifier)
	if err != nil {
		s.fail(w, err)
		return
	}
	respond(w, map[string]string{"url": u, "binding": l.Binding})
}
func (s Server) invitations(w http.ResponseWriter, r *http.Request) {
	if s.Invitations == nil {
		s.fail(w, ErrDenied)
		return
	}
	var in struct {
		Session string    `json:"session"`
		After   uuid.UUID `json:"after"`
	}
	if !decode(w, r, &in) {
		return
	}
	items, err := s.Invitations.List(r.Context(), in.Session, in.After)
	if err != nil {
		s.fail(w, err)
		return
	}
	next := ""
	if len(items) > 100 {
		items = items[:100]
		next = items[99].ID.String()
	}
	respond(w, map[string]any{"items": items, "next_after": next})
}
func (s Server) invitationChange(w http.ResponseWriter, r *http.Request) {
	if s.Invitations == nil {
		s.fail(w, ErrDenied)
		return
	}
	var in struct {
		Session string    `json:"session"`
		Action  string    `json:"action"`
		ID      uuid.UUID `json:"id"`
		Version int64     `json:"version"`
		Email   string    `json:"email"`
		Roles   []string  `json:"roles"`
	}
	if !decode(w, r, &in) {
		return
	}
	var err error
	if in.Action == "create" {
		_, err = s.Invitations.Create(r.Context(), in.Session, in.ID, in.Email, in.Roles)
	} else {
		err = s.Invitations.Change(r.Context(), in.Session, in.ID, in.Version, in.Action)
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(204)
}
