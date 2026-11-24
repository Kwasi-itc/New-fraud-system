package platform

import (
	"encoding/json"
	"net/http"

	auth "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
	"github.com/google/uuid"
)

func (s Server) accountAuditList(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	if err := checkCurrentCapability(r.Context(), s.Store.DB, p, "audit:read", false); err != nil {
		s.authorizationError(w, err)
		return
	}
	after, err := optionalUUID(r.URL.Query().Get("after"))
	if err != nil {
		http.Error(w, "invalid cursor", 400)
		return
	}
	rows, err := s.Store.DB.Query(r.Context(), `SELECT id,actor_id,action,target_id,created_at::text,details FROM identity.audit_events WHERE tenant_id=$1 AND id>$2 ORDER BY id LIMIT 101`, p.TenantID, after)
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
func (s Server) accountInvitations(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	if s.Invitations == nil {
		s.authorizationError(w, ErrDenied)
		return
	}
	after, err := optionalUUID(r.URL.Query().Get("after"))
	if err != nil {
		http.Error(w, "invalid cursor", 400)
		return
	}
	items, err := s.Invitations.ListForPrincipal(r.Context(), p, after)
	if err != nil {
		s.authorizationError(w, err)
		return
	}
	next := ""
	if len(items) > 100 {
		items = items[:100]
		next = items[99].ID.String()
	}
	respond(w, map[string]any{"items": items, "next_after": next})
}
func (s Server) accountInvitationChange(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	if s.Invitations == nil {
		s.authorizationError(w, ErrDenied)
		return
	}
	var in struct {
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
		_, err = s.Invitations.CreateForPrincipal(r.Context(), p, in.ID, in.Email, in.Roles)
	} else {
		err = s.Invitations.ChangeForPrincipal(r.Context(), p, in.ID, in.Version, in.Action)
	}
	if err != nil {
		s.Logger.Warn("invitation operation denied",
			"tenant_id", p.TenantID,
			"actor_id", p.Subject,
			"action", in.Action,
			"error", err)
		s.authorizationError(w, err)
		return
	}
	w.WriteHeader(204)
}
