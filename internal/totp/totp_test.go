package totp

import (
	"encoding/base32"
	"strings"
	"testing"
	"time"
)

// RFC 6238 appendix B, SHA-1 (the last six digits of its eight-digit codes).
func TestRFCVectors(t *testing.T) {
	secret := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString([]byte("12345678901234567890"))
	for at, want := range map[int64]string{59: "287082", 1111111109: "081804", 1234567890: "005924", 2000000000: "279037"} {
		got, err := Code(secret, at/period)
		if err != nil || got != want {
			t.Errorf("t=%d: %s %v, want %s", at, got, err, want)
		}
	}
}

func TestVerifyDriftAndReplay(t *testing.T) {
	s, err := NewSecret()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_800_000_000, 0)
	prev, _ := Code(s, Step(now)-1)
	step, ok := Verify(s, prev, now, 0)
	if !ok || step != Step(now)-1 {
		t.Fatal("one step of drift is accepted")
	}
	if _, ok := Verify(s, prev, now, step); ok {
		t.Fatal("a used code works twice")
	}
	old, _ := Code(s, Step(now)-3)
	if _, ok := Verify(s, old, now, 0); ok {
		t.Fatal("an old code is accepted")
	}
	if !strings.HasPrefix(URI("Keystone", "thijs", s), "otpauth://totp/Keystone:thijs?") {
		t.Fatal("uri")
	}
}
