// Package samlidp is Keystone's SAML 2.0 identity provider (spec §2, §8):
// signed responses and assertions for the apps configured with protocol saml.
// It wraps github.com/crewjam/saml for request parsing, assertion building
// and signing, and leaves sessions and HTTP plumbing to the caller.
package samlidp

import (
	"bytes"
	"compress/flate"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/crewjam/saml"

	"github.com/civr/keystone/internal/apps"
	"github.com/civr/keystone/internal/keys"
)

// maxRequest caps a decoded AuthnRequest.
const maxRequest = 64 << 10

// Paths under public_url.
const (
	MetadataPath = "/saml/metadata"
	SSOPath      = "/saml/sso"
)

// Request is a parsed-but-unvalidated AuthnRequest. It is JSON-serializable so
// it can wait in Valkey while the user signs in.
type Request struct {
	XML        []byte    `json:"xml"`
	RelayState string    `json:"relay_state"`
	At         time.Time `json:"at"` // when it arrived; the issue-time check uses this
}

// Parse reads an AuthnRequest from the Redirect (GET, deflated) or POST binding.
func Parse(r *http.Request) (Request, error) {
	var b64, relay string
	switch r.Method {
	case http.MethodGet:
		b64, relay = r.URL.Query().Get("SAMLRequest"), r.URL.Query().Get("RelayState")
	case http.MethodPost:
		r.Body = http.MaxBytesReader(nil, r.Body, 4*maxRequest)
		if err := r.ParseForm(); err != nil {
			return Request{}, err
		}
		b64, relay = r.PostForm.Get("SAMLRequest"), r.PostForm.Get("RelayState")
	default:
		return Request{}, errors.New("samlidp: method not allowed")
	}
	if b64 == "" {
		return Request{}, errors.New("samlidp: no SAMLRequest")
	}
	if len(relay) > 1024 {
		return Request{}, errors.New("samlidp: RelayState too long")
	}
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return Request{}, fmt.Errorf("samlidp: decode request: %w", err)
	}
	if r.Method == http.MethodGet {
		if raw, err = io.ReadAll(io.LimitReader(flate.NewReader(bytes.NewReader(raw)), maxRequest+1)); err != nil {
			return Request{}, fmt.Errorf("samlidp: inflate request: %w", err)
		}
	}
	if len(raw) > maxRequest {
		return Request{}, errors.New("samlidp: request too large")
	}
	return Request{XML: raw, RelayState: relay, At: time.Now()}, nil
}

// Form is what the browser posts to the app's ACS.
type Form struct {
	URL, SAMLResponse, RelayState string
}

// Provider builds responses from the current keys and apps.
type Provider struct {
	Keys *keys.Store
	Apps apps.Querier
}

// spProvider serves SP metadata synthesized from Keystone's app config: one
// POST-binding ACS, so an AuthnRequest naming any other ACS URL is refused.
type spProvider struct{ list []apps.App }

func (p spProvider) find(entityID string) (apps.App, bool) {
	for _, a := range p.list {
		if a.Protocol == apps.SAMLP && a.Config.SAML != nil && a.Config.SAML.EntityID == entityID {
			return a, true
		}
	}
	return apps.App{}, false
}

func (p spProvider) GetServiceProvider(_ *http.Request, id string) (*saml.EntityDescriptor, error) {
	a, ok := p.find(id)
	if !ok {
		return nil, os.ErrNotExist
	}
	return spMetadata(a), nil
}

func spMetadata(a apps.App) *saml.EntityDescriptor {
	return &saml.EntityDescriptor{
		EntityID: a.Config.SAML.EntityID,
		SPSSODescriptors: []saml.SPSSODescriptor{{
			AssertionConsumerServices: []saml.IndexedEndpoint{{
				Binding: saml.HTTPPostBinding, Location: a.Config.SAML.ACSURL, Index: 1,
			}},
		}},
	}
}

func (p *Provider) idp(ctx context.Context, publicURL string, list []apps.App) (*saml.IdentityProvider, []keys.Key, error) {
	pub, err := p.Keys.Published(ctx, keys.SAML)
	if err != nil {
		return nil, nil, err
	}
	base, err := url.Parse(strings.TrimRight(publicURL, "/"))
	if err != nil || base.Host == "" {
		return nil, nil, errors.New("samlidp: public_url is not set")
	}
	meta, sso := *base, *base
	meta.Path, sso.Path = base.Path+MetadataPath, base.Path+SSOPath
	valid := 7 * 24 * time.Hour
	return &saml.IdentityProvider{
		Key:                     pub[0].Signer,
		Signer:                  pub[0].Signer,
		Certificate:             pub[0].Cert,
		MetadataURL:             meta,
		SSOURL:                  sso,
		ServiceProviderProvider: spProvider{list},
		SignatureMethod:         "http://www.w3.org/2001/04/xmldsig-more#rsa-sha256",
		ValidDuration:           &valid,
	}, pub, nil
}

// Metadata returns the IdP metadata XML, listing every published certificate
// so SPs keep accepting the previous key for one rotation.
func (p *Provider) Metadata(ctx context.Context, publicURL string) ([]byte, error) {
	idp, pub, err := p.idp(ctx, publicURL, nil)
	if err != nil {
		return nil, err
	}
	md := idp.Metadata()
	d := &md.IDPSSODescriptors[0]
	for _, k := range pub[1:] {
		d.KeyDescriptors = append(d.KeyDescriptors, saml.KeyDescriptor{
			Use: "signing",
			KeyInfo: saml.KeyInfo{X509Data: saml.X509Data{X509Certificates: []saml.X509Certificate{
				{Data: base64.StdEncoding.EncodeToString(k.Cert.Raw)},
			}}},
		})
	}
	out, err := xml.MarshalIndent(md, "", "  ")
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), out...), nil
}

// ErrUnknownSP means no saml app has the request's issuer as entity ID.
var ErrUnknownSP = errors.New("samlidp: unknown service provider")

// Login describes the Keystone sign-in behind a response.
type Login struct {
	SessionID string
	AuthAt    time.Time
	AMR       []string // RFC 8176 methods, e.g. pwd, hwk
}

func session(acct apps.Account, l Login) *saml.Session {
	sessionID, authAt := l.SessionID, l.AuthAt
	return &saml.Session{
		ID:             sessionID,
		CreateTime:     authAt,
		Index:          sessionID[:min(16, len(sessionID))],
		NameID:         acct.Username,
		NameIDFormat:   "urn:oasis:names:tc:SAML:1.1:nameid-format:unspecified",
		UserName:       acct.Username,
		UserEmail:      acct.Email,
		UserCommonName: acct.DisplayName,
		CustomAttributes: []saml.Attribute{
			basicAttr("username", acct.Username),
			basicAttr("email", acct.Email),
			basicAttr("displayName", acct.DisplayName),
			multiAttr("amr", l.AMR),
		},
	}
}

func multiAttr(name string, vals []string) saml.Attribute {
	a := saml.Attribute{Name: name, NameFormat: "urn:oasis:names:tc:SAML:2.0:attrname-format:basic"}
	for _, v := range vals {
		a.Values = append(a.Values, saml.AttributeValue{Type: "xs:string", Value: v})
	}
	return a
}

func basicAttr(name, v string) saml.Attribute {
	return saml.Attribute{
		Name: name, NameFormat: "urn:oasis:names:tc:SAML:2.0:attrname-format:basic",
		Values: []saml.AttributeValue{{Type: "xs:string", Value: v}},
	}
}

// Check validates req (known SP, ACS, issue time) without answering it, so a
// request can be refused before it waits for the user to sign in.
func (p *Provider) Check(ctx context.Context, publicURL string, r *http.Request, req Request) error {
	list, err := apps.Load(ctx, p.Apps)
	if err != nil {
		return err
	}
	idp, _, err := p.idp(ctx, publicURL, list)
	if err != nil {
		return err
	}
	ar := &saml.IdpAuthnRequest{IDP: idp, HTTPRequest: r, RequestBuffer: req.XML, RelayState: req.RelayState, Now: req.At}
	if err := ar.Validate(); err != nil {
		if strings.Contains(err.Error(), "unknown service provider") {
			return ErrUnknownSP
		}
		return err
	}
	return nil
}

// Respond validates an SP-initiated request and returns the signed response
// for the matching app, plus that app.
func (p *Provider) Respond(ctx context.Context, publicURL string, r *http.Request, req Request,
	owner apps.Owner, login Login) (Form, apps.App, error) {
	list, err := apps.Load(ctx, p.Apps)
	if err != nil {
		return Form{}, apps.App{}, err
	}
	idp, _, err := p.idp(ctx, publicURL, list)
	if err != nil {
		return Form{}, apps.App{}, err
	}
	ar := &saml.IdpAuthnRequest{IDP: idp, HTTPRequest: r, RequestBuffer: req.XML, RelayState: req.RelayState, Now: req.At}
	if err := ar.Validate(); err != nil {
		if strings.Contains(err.Error(), "unknown service provider") {
			return Form{}, apps.App{}, ErrUnknownSP
		}
		return Form{}, apps.App{}, err
	}
	a, _ := spProvider{list}.find(ar.Request.Issuer.Value)
	ar.Now = time.Now()
	return p.finish(ar, a, owner, login)
}

// RespondIdPInitiated builds an unsolicited response for app a.
func (p *Provider) RespondIdPInitiated(ctx context.Context, publicURL string, r *http.Request, a apps.App,
	owner apps.Owner, login Login) (Form, error) {
	idp, _, err := p.idp(ctx, publicURL, []apps.App{a})
	if err != nil {
		return Form{}, err
	}
	md := spMetadata(a)
	ar := &saml.IdpAuthnRequest{
		IDP: idp, HTTPRequest: r, Now: time.Now(),
		ServiceProviderMetadata: md,
		SPSSODescriptor:         &md.SPSSODescriptors[0],
		ACSEndpoint:             &md.SPSSODescriptors[0].AssertionConsumerServices[0],
	}
	f, _, err := p.finish(ar, a, owner, login)
	return f, err
}

func (p *Provider) finish(ar *saml.IdpAuthnRequest, a apps.App, owner apps.Owner, login Login) (Form, apps.App, error) {
	if err := (saml.DefaultAssertionMaker{}).MakeAssertion(ar, session(a.Mapped(owner), login)); err != nil {
		return Form{}, a, err
	}
	f, err := ar.PostBinding()
	if err != nil {
		return Form{}, a, err
	}
	return Form{URL: f.URL, SAMLResponse: f.SAMLResponse, RelayState: f.RelayState}, a, nil
}

const submitScript = `document.forms[0].submit()`

var postPage = template.Must(template.New("post").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>Redirecting…</title></head>
<body style="font-family:system-ui,sans-serif;background:#f5f7fb;color:#5b6475">
<form method="post" action="{{.URL}}">
<input type="hidden" name="SAMLResponse" value="{{.SAMLResponse}}">
{{if .RelayState}}<input type="hidden" name="RelayState" value="{{.RelayState}}">{{end}}
<noscript><button type="submit">Continue</button></noscript>
</form>
<script>` + submitScript + `</script>
</body></html>`))

// CSP returns the policy for the auto-post page: only its own submit script
// runs, and the form may only go to the app's ACS origin.
func CSP(acsURL string) string {
	u, _ := url.Parse(acsURL)
	return "default-src 'none'; script-src '" + scriptHash() + "'; style-src 'unsafe-inline'; " +
		"form-action " + u.Scheme + "://" + u.Host + "; frame-ancestors 'none'; base-uri 'none'"
}

// WriteForm writes the auto-posting page.
func WriteForm(w http.ResponseWriter, f Form) error {
	h := w.Header()
	h.Set("Content-Security-Policy", CSP(f.URL))
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	return postPage.Execute(w, f)
}

func scriptHash() string {
	sum := sha256.Sum256([]byte(submitScript))
	return "sha256-" + base64.StdEncoding.EncodeToString(sum[:])
}
