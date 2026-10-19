// Package secrets generates and loads the deployment secrets that live in a
// dedicated volume (mounted at /run/keystone). They are never stored in Postgres.
package secrets

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Files lists the secret file names Ensure manages.
var Files = []string{"db_password", "valkey_password", "session_key", "data_key"}

// Secrets holds the loaded deployment secrets.
type Secrets struct {
	DBPassword     string
	ValkeyPassword string
	SessionKey     []byte // 32 bytes
	DataKey        []byte // 32 bytes
}

func isPassword(name string) bool { return name == "db_password" || name == "valkey_password" }

// Ensure creates any missing secret file in dir with mode 0400 and never
// overwrites an existing one.
func Ensure(dir string) error {
	for _, name := range Files {
		raw := make([]byte, 32)
		if _, err := rand.Read(raw); err != nil {
			return fmt.Errorf("secrets: random: %w", err)
		}
		content := raw
		if isPassword(name) {
			content = []byte(base64.RawURLEncoding.EncodeToString(raw))
		}
		if err := writeNew(filepath.Join(dir, name), content); err != nil {
			return fmt.Errorf("secrets: %s: %w", name, err)
		}
	}
	return nil
}

func writeNew(path string, content []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o400)
	if errors.Is(err, fs.ErrExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := f.Write(content); err != nil {
		f.Close()
		os.Remove(path)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(path)
		return err
	}
	return nil
}

// Load reads all secret files from dir.
func Load(dir string) (Secrets, error) {
	var s Secrets
	read := func(name string) ([]byte, error) {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, fmt.Errorf("secrets: read %s: %w", name, err)
		}
		return b, nil
	}
	b, err := read("db_password")
	if err != nil {
		return s, err
	}
	s.DBPassword = string(b)
	if b, err = read("valkey_password"); err != nil {
		return s, err
	}
	s.ValkeyPassword = string(b)
	if s.SessionKey, err = read("session_key"); err != nil {
		return s, err
	}
	if s.DataKey, err = read("data_key"); err != nil {
		return s, err
	}
	for name, k := range map[string][]byte{"session_key": s.SessionKey, "data_key": s.DataKey} {
		if len(k) != 32 {
			return s, fmt.Errorf("secrets: %s has %d bytes, want 32", name, len(k))
		}
	}
	return s, nil
}
