package platform

func (s Server) accountRoutes(mux *routeRegistry) {
	tenant := "/v1/tenants/{tenantId}"
	mux.HandleProtected("GET /v1/account/profile", s.authenticated(s.profile))
	mux.HandleProtected("PUT /v1/account/profile", s.authenticated(s.changeProfile))
	mux.HandleProtected("POST /v1/account/logout", s.authenticated(s.accountLogout))
	mux.HandleProtected("GET /v1/account/sessions", s.authenticated(s.accountSessions))
	mux.HandleProtected("POST /v1/account/sessions/revoke", s.authenticated(s.accountRevokeSessions))

	mux.HandleProtected("GET "+tenant+"/members", s.protected("members:read", s.accountMembers))
	mux.HandleProtected("POST "+tenant+"/members", s.protected("roles:assign", s.accountMemberChange))
	mux.HandleProtected("GET "+tenant+"/invitations", s.protected("invitations:manage", s.accountInvitations))
	mux.HandleProtected("POST "+tenant+"/invitations", s.protected("invitations:manage", s.accountInvitationChange))
	mux.HandleProtected("GET "+tenant+"/audit", s.protected("audit:read", s.accountAuditList))
}
