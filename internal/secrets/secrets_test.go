package secrets

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)


func TestEnsureCreatesAllFiles(t *testing.T) {
	dir := t.TempDir()
	if err := Ensure(dir); err != nil {
		t.Fatal(err)
	}
	for _, name := range Files {
		fi, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if fi.Mode().Perm() != 0o400 {
			t.Errorf("%s mode = %v, want 0400", name, fi.Mode().Perm())
		}
	}
	s, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.SessionKey) != 32 || len(s.DataKey) != 32 {
		t.Errorf("key lengths = %d, %d; want 32", len(s.SessionKey), len(s.DataKey))
	}
	for name, pw := range map[string]string{"db": s.DBPassword, "valkey": s.ValkeyPassword} {
		if len(pw) < 32 || !urlSafe.MatchString(pw) {
			t.Errorf("%s password %q not >=32 URL-safe chars", name, pw)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "valkey.conf")); err == nil {
		t.Error("valkey.conf must not be written")
	}
}

func TestEnsureIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	if err := Ensure(dir); err != nil {
		t.Fatal(err)
	}
	before := map[string][]byte{}
	for _, name := range Files {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		before[name] = b
	}
	if err := Ensure(dir); err != nil {
		t.Fatal(err)
	}
	for _, name := range Files {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if string(b) != string(before[name]) {
			t.Errorf("%s changed on second Ensure", name)
		}
	}
}

func TestEnsureFillsOnlyMissing(t *testing.T) {
	dir := t.TempDir()
	if err := Ensure(dir); err != nil {
		t.Fatal(err)
	}
	orig, _ := os.ReadFile(filepath.Join(dir, "db_password"))
	if err := os.Remove(filepath.Join(dir, "data_key")); err != nil {
		t.Fatal(err)
	}
	if err := Ensure(dir); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "db_password")); string(b) != string(orig) {
		t.Error("existing db_password was overwritten")
	}
	if _, err := Load(dir); err != nil {
		t.Errorf("data_key not recreated: %v", err)
	}
}

func TestLoadMissingFileErrors(t *testing.T) {
	dir := t.TempDir()
	if err := Ensure(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "session_key")); err != nil {
		t.Fatal(err)
	}
	_, err := Load(dir)
	if err == nil || !strings.Contains(err.Error(), "session_key") {
		t.Fatalf("err = %v, want mention of session_key", err)
	}
}

func TestEnsureRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(t.TempDir(), "elsewhere")
	if err := os.WriteFile(target, make([]byte, 32), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "data_key")); err != nil {
		t.Fatal(err)
	}
	err := Ensure(dir)
	if err == nil || !strings.Contains(err.Error(), "data_key") {
		t.Fatalf("err = %v, want error naming data_key", err)
	}
}

func TestEnsureRejectsEmptyExistingKey(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "session_key"), nil, 0o400); err != nil {
		t.Fatal(err)
	}
	err := Ensure(dir)
	if err == nil || !strings.Contains(err.Error(), "session_key") {
		t.Fatalf("err = %v, want error naming session_key", err)
	}
}

func TestLoadRejectsShortPassword(t *testing.T) {
	dir := t.TempDir()
	if err := Ensure(dir); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "valkey_password")
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("short"), 0o400); err != nil {
		t.Fatal(err)
	}
	_, err := Load(dir)
	if err == nil || !strings.Contains(err.Error(), "valkey_password") {
		t.Fatalf("err = %v, want error naming valkey_password", err)
	}
}
