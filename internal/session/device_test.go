package session

import (
	"strings"
	"testing"
	"time"

	"github.com/civr/keystone/internal/settings"
)

var devKey = []byte("0123456789abcdef0123456789abcdef")

func TestDeviceRoundTrip(t *testing.T) {
	cfg := settings.Defaults()
	c := SignDevice(devKey, "dev-1", t0)
	id, ok := VerifyDevice(devKey, c, cfg, t0.Add(time.Hour))
	if !ok || id != "dev-1" {
		t.Fatalf("got %q %v", id, ok)
	}
	if _, ok := VerifyDevice([]byte("another-key-another-key-another!"), c, cfg, t0); ok {
		t.Fatal("wrong key accepted")
	}
}

func TestDeviceCookieTamperRejected(t *testing.T) {
	cfg := settings.Defaults()
	c := SignDevice(devKey, "dev-1", t0)
	forged := SignDevice(devKey, "dev-2", t0)
	payloadOfForged, _, _ := strings.Cut(forged, ".")
	_, sig, _ := strings.Cut(c, ".")
	for name, bad := range map[string]string{
		"swapped payload": payloadOfForged + "." + sig,
		"truncated sig":   c[:len(c)-2],
		"no dot":          strings.ReplaceAll(c, ".", ""),
		"empty":           "",
		"garbage":         "!!!.???",
	} {
		if _, ok := VerifyDevice(devKey, bad, cfg, t0); ok {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestDeviceExpiresAfterTrustedDays(t *testing.T) {
	cfg := settings.Defaults()
	cfg.Session.TrustedDeviceDays = 90
	c := SignDevice(devKey, "dev-1", t0)
	if _, ok := VerifyDevice(devKey, c, cfg, t0.Add(89*24*time.Hour)); !ok {
		t.Fatal("should be valid at day 89")
	}
	if _, ok := VerifyDevice(devKey, c, cfg, t0.Add(91*24*time.Hour)); ok {
		t.Fatal("should expire after day 90")
	}
	cfg.Session.TrustedDeviceDays = 30 // shortening applies immediately
	if _, ok := VerifyDevice(devKey, c, cfg, t0.Add(31*24*time.Hour)); ok {
		t.Fatal("shortened setting not applied")
	}
}
