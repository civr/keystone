package passkey

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/go-webauthn/webauthn/protocol/webauthncbor"
	"github.com/go-webauthn/webauthn/protocol/webauthncose"
)

// TestAuthenticator is a minimal software authenticator for tests: an ECDSA
// P-256 key with "none" attestation that answers creation and request options
// the way a browser + platform authenticator would, so tests exercise the real
// go-webauthn verification. Not for production use.
type TestAuthenticator struct {
	Origin     string // origin written into clientDataJSON
	Counter    uint32 // signature counter; incremented before every assertion
	CredID     []byte
	UserHandle []byte // set by Register from the creation options

	key *ecdsa.PrivateKey
}

// NewTestAuthenticator returns an authenticator acting for origin.
func NewTestAuthenticator(origin string) *TestAuthenticator {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		panic(err)
	}
	id := make([]byte, 32)
	if _, err := rand.Read(id); err != nil {
		panic(err)
	}
	return &TestAuthenticator{Origin: origin, CredID: id, key: key}
}

const (
	flagUP = 0x01
	flagUV = 0x04
	flagAT = 0x40
)

var b64 = base64.RawURLEncoding

// b64bytes decodes a base64url JSON string, padded or not.
type b64bytes []byte

func (b *b64bytes) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	v, err := base64.RawURLEncoding.DecodeString(trimPad(s))
	*b = v
	return err
}

func trimPad(s string) string {
	for len(s) > 0 && s[len(s)-1] == '=' {
		s = s[:len(s)-1]
	}
	return s
}

func (a *TestAuthenticator) clientData(typ string, challenge []byte) []byte {
	cd, _ := json.Marshal(map[string]any{
		"type": typ, "challenge": b64.EncodeToString(challenge), "origin": a.Origin, "crossOrigin": false,
	})
	return cd
}

func authData(rpID string, flags byte, counter uint32, attested []byte) []byte {
	h := sha256.Sum256([]byte(rpID))
	out := append([]byte{}, h[:]...)
	out = append(out, flags)
	out = binary.BigEndian.AppendUint32(out, counter)
	return append(out, attested...)
}

// Register answers credential creation options (the JSON from BeginRegistration)
// and returns the body navigator.credentials.create() would produce.
func (a *TestAuthenticator) Register(options []byte) ([]byte, error) {
	var opts struct {
		PublicKey struct {
			Challenge b64bytes `json:"challenge"`
			RP        struct {
				ID string `json:"id"`
			} `json:"rp"`
			User struct {
				ID b64bytes `json:"id"`
			} `json:"user"`
		} `json:"publicKey"`
	}
	if err := json.Unmarshal(options, &opts); err != nil {
		return nil, fmt.Errorf("testauth: creation options: %w", err)
	}
	if opts.PublicKey.RP.ID == "" || len(opts.PublicKey.Challenge) == 0 {
		return nil, errors.New("testauth: creation options without rp.id or challenge")
	}
	a.UserHandle = opts.PublicKey.User.ID

	cose, err := webauthncbor.Marshal(webauthncose.EC2PublicKeyData{
		PublicKeyData: webauthncose.PublicKeyData{KeyType: int64(webauthncose.EllipticKey), Algorithm: int64(webauthncose.AlgES256)},
		Curve:         int64(webauthncose.P256),
		XCoord:        a.key.PublicKey.X.FillBytes(make([]byte, 32)),
		YCoord:        a.key.PublicKey.Y.FillBytes(make([]byte, 32)),
	})
	if err != nil {
		return nil, err
	}
	attested := make([]byte, 16) // zero AAGUID
	attested = binary.BigEndian.AppendUint16(attested, uint16(len(a.CredID)))
	attested = append(attested, a.CredID...)
	attested = append(attested, cose...)
	ad := authData(opts.PublicKey.RP.ID, flagUP|flagUV|flagAT, a.Counter, attested)
	attObj, err := webauthncbor.Marshal(map[string]any{"fmt": "none", "attStmt": map[string]any{}, "authData": ad})
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{
		"id": b64.EncodeToString(a.CredID), "rawId": b64.EncodeToString(a.CredID), "type": "public-key",
		"authenticatorAttachment": "platform", "clientExtensionResults": map[string]any{},
		"response": map[string]any{
			"clientDataJSON":    b64.EncodeToString(a.clientData("webauthn.create", opts.PublicKey.Challenge)),
			"attestationObject": b64.EncodeToString(attObj),
			"transports":        []string{"internal"},
		},
	})
}

// Assert answers credential request options (the JSON from BeginLogin) and
// returns the body navigator.credentials.get() would produce.
func (a *TestAuthenticator) Assert(options []byte) ([]byte, error) {
	var opts struct {
		PublicKey struct {
			Challenge b64bytes `json:"challenge"`
			RPID      string   `json:"rpId"`
		} `json:"publicKey"`
	}
	if err := json.Unmarshal(options, &opts); err != nil {
		return nil, fmt.Errorf("testauth: request options: %w", err)
	}
	if opts.PublicKey.RPID == "" || len(opts.PublicKey.Challenge) == 0 {
		return nil, errors.New("testauth: request options without rpId or challenge")
	}
	a.Counter++
	ad := authData(opts.PublicKey.RPID, flagUP|flagUV, a.Counter, nil)
	cd := a.clientData("webauthn.get", opts.PublicKey.Challenge)
	cdHash := sha256.Sum256(cd)
	digest := sha256.Sum256(append(append([]byte{}, ad...), cdHash[:]...))
	sig, err := ecdsa.SignASN1(rand.Reader, a.key, digest[:])
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{
		"id": b64.EncodeToString(a.CredID), "rawId": b64.EncodeToString(a.CredID), "type": "public-key",
		"authenticatorAttachment": "platform", "clientExtensionResults": map[string]any{},
		"response": map[string]any{
			"clientDataJSON":    b64.EncodeToString(cd),
			"authenticatorData": b64.EncodeToString(ad),
			"signature":         b64.EncodeToString(sig),
			"userHandle":        b64.EncodeToString(a.UserHandle),
		},
	})
}
