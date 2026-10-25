package httpapi

import (
	"crypto/subtle"
	"net/http"
	"sort"

	"github.com/go-chi/chi/v5"

	"github.com/civr/keystone/internal/accounts"
	"github.com/civr/keystone/internal/gatetoken"
)

// Every account's sessions, for admins with sessions.manage: see who is
// signed in where, and end a session. Ending someone's session needs at
// least that person's rights (mayManage), like any change to their account.

func (s *server) listAllSessions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	list, err := s.d.Sessions.List(ctx)
	if err != nil {
		errUnavailable(w)
		return
	}
	accts, err := accounts.List(ctx, s.d.Pool)
	if err != nil {
		errInternal(w, "list accounts", err)
		return
	}
	names := map[string]string{}
	for _, a := range accts {
		names[accountRef(a.ID)] = a.Name()
	}
	sort.Slice(list, func(i, j int) bool { return list[i].LastSeenAt.After(list[j].LastSeenAt) })
	own := SessionFrom(ctx).ID
	out := make([]map[string]any, 0, len(list))
	for _, x := range list {
		out = append(out, map[string]any{
			"id": sessionHandle(x.ID), "account": accountKey(x.UserID), "name": names[x.UserID],
			"created_at": x.CreatedAt, "last_seen_at": x.LastSeenAt, "ip": x.IP, "user_agent": x.UserAgent,
			"emergency": x.Emergency, "current": x.ID == own,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": out})
}

func (s *server) endAnySession(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	handle := chi.URLParam(r, "id")
	list, err := s.d.Sessions.List(ctx)
	if err != nil {
		errUnavailable(w)
		return
	}
	for _, x := range list {
		if subtle.ConstantTimeCompare([]byte(sessionHandle(x.ID)), []byte(handle)) != 1 {
			continue
		}
		if x.UserID != SessionFrom(ctx).UserID && !s.mayManage(w, r, accountKey(x.UserID)) {
			return
		}
		if err := s.d.Sessions.Revoke(ctx, x.ID); err != nil {
			errUnavailable(w)
			return
		}
		_ = gatetoken.RevokeSession(ctx, s.d.Valkey, x.ID)
		s.d.Audit.Log(ctx, actor(ctx), "session.revoked", clientIP(ctx), map[string]any{"account": accountKey(x.UserID), "by_admin": true})
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return
	}
	errNotFound(w, r)
}

// myAccess is what the signed-in account has: its groups, its roles and the
// apps it may use.
func (s *server) myAccess(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	acct, err := s.account(ctx)
	if err != nil {
		errInternal(w, "load account", err)
		return
	}
	type named struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	groups := []named{}
	rows, err := s.d.Pool.Query(ctx, `select g.id::text, g.name from groups g join group_members m on m.group_id = g.id
		where m.account_id = $1 order by g.name`, acct.ID)
	if err != nil {
		errInternal(w, "my groups", err)
		return
	}
	for rows.Next() {
		var n named
		if err := rows.Scan(&n.ID, &n.Name); err != nil {
			rows.Close()
			errInternal(w, "my groups", err)
			return
		}
		groups = append(groups, n)
	}
	rows.Close()
	g, err := s.grants(ctx)
	if err != nil {
		errInternal(w, "load grants", err)
		return
	}
	byID, _, err := s.loadRoles(ctx)
	if err != nil {
		errInternal(w, "load roles", err)
		return
	}
	roles := []named{}
	for _, id := range g.Roles() {
		roles = append(roles, named{ID: id, Name: byID[id].Name})
	}
	writeJSON(w, http.StatusOK, map[string]any{"groups": groups, "roles": roles, "permissions": g.List(), "app_admin_of": g.Apps()})
}
