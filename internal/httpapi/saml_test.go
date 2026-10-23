package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/crewjam/saml"
)

const (
	spEntity = "https://bastion.home.lan/api/auth/saml/metadata"
	spACS    = "https://bastion.home.lan/api/auth/saml/acs"
)

func samlApp(c *client, idpInit bool) string {
	var a protoApp
	body(c.t, c.must("POST", "/api/apps", map[string]any{
		"name": "Bastion", "url": "https://bastion.home.lan", "color": "#107C10", "protocol": "saml",
		"config": map[string]any{
			"account": map[string]any{"username": "t.willems"},
			"saml":    map[string]any{"entity_id": spEntity, "acs_url": spACS, "idp_initiated": idpInit},
		},
	}, 201).Body, &a)
	return a.ID
}

func testSP(t *testing.T, c *client) *saml.ServiceProvider {
	t.Helper()
	w := c.must("GET", "/saml/metadata", nil, 200)
	var md saml.EntityDescriptor
	if err := xml.Unmarshal(w.Body.Bytes(), &md); err != nil {
		t.Fatal(err)
	}
	acs, _ := url.Parse(spACS)
	meta, _ := url.Parse(spEntity)
	return &saml.ServiceProvider{EntityID: spEntity, AcsURL: *acs, MetadataURL: *meta, IDPMetadata: &md, AllowIDPInitiated: true}
}

var formRe = regexp.MustCompile(`<form method="post" action="([^"]+)">\s*<input type="hidden" name="SAMLResponse" value="([^"]+)">`)

// postBack parses the auto-post page and returns the assertion as the SP sees it.
func postBack(t *testing.T, sp *saml.ServiceProvider, w *httptest.ResponseRecorder, ids []string) (*saml.Assertion, error) {
	t.Helper()
	m := formRe.FindStringSubmatch(w.Body.String())
	if m == nil {
		t.Fatalf("no auto-post form (%d): %s", w.Code, w.Body.String())
	}
	if m[1] != spACS {
		t.Fatalf("form action = %q", m[1])
	}
	form := url.Values{"SAMLResponse": {html.UnescapeString(m[2])}}
	r := httptest.NewRequest("POST", spACS, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := r.ParseForm(); err != nil {
		t.Fatal(err)
	}
	as, err := sp.ParseResponse(r, ids)
	var ire *saml.InvalidResponseError
	if errors.As(err, &ire) {
		err = ire.PrivateErr
	}
	return as, err
}

func redirectRequest(t *testing.T, sp *saml.ServiceProvider) (string, string) {
	t.Helper()
	req, err := sp.MakeAuthenticationRequest(sp.GetSSOBindingLocation(saml.HTTPRedirectBinding), saml.HTTPRedirectBinding, saml.HTTPPostBinding)
	if err != nil {
		t.Fatal(err)
	}
	u, err := req.Redirect("relay-1", sp)
	if err != nil {
		t.Fatal(err)
	}
	return u.RequestURI(), req.ID
}

func TestSAML(t *testing.T) {
	t.Parallel()
	l, c := adminClient(t, "192.168.1.20")
	samlApp(c, false)
	sp := testSP(t, c)

	t.Run("MetadataHasCert", func(t *testing.T) {
		if len(sp.IDPMetadata.IDPSSODescriptors[0].KeyDescriptors) == 0 {
			t.Fatal("no signing cert")
		}
		if got := sp.GetSSOBindingLocation(saml.HTTPRedirectBinding); got != "https://keystone.home.lan/saml/sso" {
			t.Fatalf("sso = %q", got)
		}
	})

	t.Run("SPInitiatedSignedResponse", func(t *testing.T) {
		path, id := redirectRequest(t, sp)
		w := c.must("GET", path, nil, 200)
		if csp := w.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "form-action https://bastion.home.lan;") || strings.Contains(csp, "script-src 'unsafe") {
			t.Fatalf("csp = %q", csp)
		}
		as, err := postBack(t, sp, w, []string{id})
		if err != nil {
			t.Fatalf("SP rejected response: %v", err)
		}
		if as.Subject.NameID.Value != "t.willems" {
			t.Fatalf("nameid = %q", as.Subject.NameID.Value)
		}
		var amr []string
		for _, st := range as.AttributeStatements {
			for _, a := range st.Attributes {
				if a.Name == "amr" {
					for _, v := range a.Values {
						amr = append(amr, v.Value)
					}
				}
			}
		}
		if !slices.Contains(amr, "hwk") {
			t.Fatalf("amr attribute = %v, want hwk for a passkey session", amr)
		}
		if !strings.Contains(w.Body.String(), `name="RelayState" value="relay-1"`) {
			t.Fatal("relay state lost")
		}
		if l.audits(t, "app.access") == 0 {
			t.Fatal("no audit")
		}
	})

	t.Run("NoSessionRedirectsToLoginAndResumes", func(t *testing.T) {
		path, id := redirectRequest(t, sp)
		anon := newClient(t, l.env, "192.168.1.21")
		w := anon.must("GET", path, nil, 302)
		loc := w.Header().Get("Location")
		if !strings.HasPrefix(loc, "/login?next=%2Fsaml%2Fsso%3Fresume%3D") {
			t.Fatalf("location = %q", loc)
		}
		next, _ := url.QueryUnescape(strings.TrimPrefix(loc, "/login?next="))
		anon.cookies = c.cookies // signed in meanwhile
		as, err := postBack(t, sp, anon.must("GET", next, nil, 200), []string{id})
		if err != nil || as.Subject.NameID.Value != "t.willems" {
			t.Fatalf("resume: %v", err)
		}
		anon.must("GET", next, nil, 400) // single use
	})

	t.Run("PostBindingAcceptedWithoutOrigin", func(t *testing.T) {
		req, err := sp.MakeAuthenticationRequest(sp.GetSSOBindingLocation(saml.HTTPRedirectBinding), saml.HTTPPostBinding, saml.HTTPPostBinding)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := xml.Marshal(req)
		if err != nil {
			t.Fatal(err)
		}
		form := url.Values{"SAMLRequest": {base64.StdEncoding.EncodeToString(raw)}}
		r := httptest.NewRequest("POST", "/saml/sso", strings.NewReader(form.Encode()))
		r.Host = setupHost
		r.RemoteAddr = "192.168.1.20:4000"
		r.Header.Set("Origin", "https://bastion.home.lan")
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		l.h.ServeHTTP(w, r) // cross-site POST: Lax cookie absent → login detour
		if w.Code != http.StatusFound || !strings.Contains(w.Header().Get("Location"), "resume") {
			t.Fatalf("got %d %s", w.Code, w.Body.String())
		}
	})

	t.Run("UnknownSPNotStashed", func(t *testing.T) {
		other := *sp
		other.EntityID = "https://evil.example/metadata"
		path, _ := redirectRequest(t, &other)
		anon := newClient(t, l.env, "192.168.1.24")
		if w := anon.do("GET", path, nil); w.Code != 400 {
			t.Fatalf("unknown SP without session = %d %s", w.Code, w.Header().Get("Location"))
		}
		keys, _ := l.deps.Valkey.Keys(context.Background(), "samlreq:*").Result()
		for _, k := range keys {
			raw, _ := l.deps.Valkey.Get(context.Background(), k).Result()
			if strings.Contains(raw, base64.StdEncoding.EncodeToString([]byte("evil.example"))[:8]) {
				t.Fatal("request of an unknown SP was stashed")
			}
		}
	})

	t.Run("StashRateLimited", func(t *testing.T) {
		anon := newClient(t, l.env, "192.168.1.25")
		limited := false
		for range 40 {
			path, _ := redirectRequest(t, sp)
			if w := anon.do("GET", path, nil); w.Code == 429 {
				limited = true
				break
			}
		}
		if !limited {
			t.Fatal("no rate limit on stashing SAML requests")
		}
	})

	t.Run("UnknownSPRejected", func(t *testing.T) {
		other := *sp
		other.EntityID = "https://evil.example/metadata"
		path, _ := redirectRequest(t, &other)
		if w := c.do("GET", path, nil); w.Code != 400 || strings.Contains(w.Body.String(), "SAMLResponse") {
			t.Fatalf("got %d", w.Code)
		}
	})

	t.Run("ACSMismatchRejected", func(t *testing.T) {
		other := *sp
		evil, _ := url.Parse("https://evil.example/acs")
		other.AcsURL = *evil
		path, _ := redirectRequest(t, &other)
		if w := c.do("GET", path, nil); w.Code != 400 || strings.Contains(w.Body.String(), "SAMLResponse") {
			t.Fatalf("got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("IdPInitiatedDisabledIs404", func(t *testing.T) {
		var list struct {
			Apps []protoApp `json:"apps"`
		}
		body(t, c.must("GET", "/api/apps", nil, 200).Body, &list)
		c.must("GET", "/saml/idp/"+list.Apps[0].ID, nil, 404)
	})
}

func TestSAMLIdPInitiated(t *testing.T) {
	t.Parallel()
	_, c := adminClient(t, "192.168.1.20")
	id := samlApp(c, true)
	sp := testSP(t, c)
	as, err := postBack(t, sp, c.must("GET", "/saml/idp/"+id, nil, 200), nil)
	if err != nil || as.Subject.NameID.Value != "t.willems" {
		t.Fatalf("idp-initiated: %v", err)
	}
}
