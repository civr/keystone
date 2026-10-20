package session

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"strconv"
	"strings"
	"time"

	"github.com/civr/keystone/internal/settings"
)

var b64 = base64.RawURLEncoding

func macOf(key, payload []byte) []byte {
	m := hmac.New(sha256.New, key)
	m.Write(payload)
	return m.Sum(nil)
}

// SignDevice builds the ks_device cookie value:
// base64url(deviceID|unix(lastFullAuth)) "." base64url(HMAC-SHA256).
func SignDevice(key []byte, deviceID string, lastFullAuth time.Time) string {
	payload := []byte(deviceID + "|" + strconv.FormatInt(lastFullAuth.Unix(), 10))
	return b64.EncodeToString(payload) + "." + b64.EncodeToString(macOf(key, payload))
}

// VerifyDevice checks the signature and that the last full authentication is
// within TrustedDeviceDays of now.
func VerifyDevice(key []byte, cookie string, cfg settings.Settings, now time.Time) (string, bool) {
	id, _, ok := VerifyDeviceAt(key, cookie, cfg, now)
	return id, ok
}

// VerifyDeviceAt is VerifyDevice that also returns the signed time of the last
// full authentication (second precision).
func VerifyDeviceAt(key []byte, cookie string, cfg settings.Settings, now time.Time) (string, time.Time, bool) {
	p, sig, ok := strings.Cut(cookie, ".")
	if !ok {
		return "", time.Time{}, false
	}
	payload, err := b64.DecodeString(p)
	if err != nil {
		return "", time.Time{}, false
	}
	got, err := b64.DecodeString(sig)
	if err != nil || !hmac.Equal(got, macOf(key, payload)) {
		return "", time.Time{}, false
	}
	i := strings.LastIndexByte(string(payload), '|')
	if i < 0 {
		return "", time.Time{}, false
	}
	secs, err := strconv.ParseInt(string(payload[i+1:]), 10, 64)
	if err != nil {
		return "", time.Time{}, false
	}
	at := time.Unix(secs, 0)
	if now.Sub(at) >= time.Duration(cfg.Session.TrustedDeviceDays)*24*time.Hour {
		return "", time.Time{}, false
	}
	return string(payload[:i]), at, true
}
