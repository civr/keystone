package httpapi

import (
	"net/http"
	"slices"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/civr/keystone/internal/audit"
	"github.com/civr/keystone/internal/keys"
)

type keyOut struct {
	Use       string     `json:"use"`
	ID        string     `json:"id"`
	Active    bool       `json:"active"`
	CreatedAt time.Time  `json:"created_at"`
	RetiredAt *time.Time `json:"retired_at"`
	CertPEM   string     `json:"cert_pem"`
}

var keyUses = []string{keys.SAML, keys.OIDC, keys.LDAP}

// listKeys returns the published signing keys (active first) per use.
func (s *server) listKeys(w http.ResponseWriter, r *http.Request) {
	out := []keyOut{}
	for _, use := range keyUses {
		pub, err := s.keys.Published(r.Context(), use)
		if err != nil {
			errInternal(w, "list keys", err)
			return
		}
		for i, k := range pub {
			out = append(out, keyOut{Use: use, ID: k.ID, Active: i == 0, CreatedAt: k.CreatedAt, RetiredAt: k.RetiredAt, CertPEM: k.CertPEM})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"keys": out})
}

func (s *server) rotateKey(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	use := chi.URLParam(r, "use")
	if !slices.Contains(keyUses, use) {
		errNotFound(w, r)
		return
	}
	k, err := s.keys.Rotate(ctx, use)
	if err != nil {
		errInternal(w, "rotate key", err)
		return
	}
	s.d.Audit.Log(ctx, audit.ActorOwner, "keys.rotated", clientIP(ctx), map[string]any{"use": use, "id": k.ID})
	writeJSON(w, http.StatusOK, map[string]string{"id": k.ID})
}
