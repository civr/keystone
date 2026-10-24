package httpapi

import (
	"strings"
	"testing"
)

func TestVaultItems(t *testing.T) {
	t.Parallel()
	l, c := adminClient(t, "192.168.1.20")

	c.must("POST", "/api/vault", map[string]any{"title": " "}, 400)
	c.must("POST", "/api/vault", map[string]any{"title": "Router", "url": "javascript:alert(1)"}, 400)

	var it map[string]any
	body(t, c.must("POST", "/api/vault", map[string]any{
		"title": "Router", "url": "http://192.168.1.1", "username": "admin", "password": "hunter2", "note": "rack 2",
	}, 201).Body, &it)
	id, _ := it["id"].(string)
	if it["has_password"] != true || it["has_note"] != true || strings.Contains(c.must("GET", "/api/vault", nil, 200).Body.String(), "hunter2") {
		t.Fatalf("created = %v", it)
	}

	var secret map[string]string
	body(t, c.must("POST", "/api/vault/"+id+"/reveal", nil, 200).Body, &secret)
	if secret["password"] != "hunter2" || secret["note"] != "rack 2" {
		t.Fatalf("reveal = %v", secret)
	}
	var raw string
	if err := l.deps.Pool.QueryRow(t.Context(), `select password_sealed from vault_items where id = $1`, id).Scan(&raw); err != nil || strings.Contains(raw, "hunter2") {
		t.Fatalf("stored = %q, %v", raw, err)
	}

	// Omitted secrets are kept; "" clears.
	c.must("PUT", "/api/vault/"+id, map[string]any{"title": "Router 2", "username": "root"}, 200)
	body(t, c.must("POST", "/api/vault/"+id+"/reveal", nil, 200).Body, &secret)
	if secret["password"] != "hunter2" {
		t.Fatalf("kept = %v", secret)
	}
	body(t, c.must("PUT", "/api/vault/"+id, map[string]any{"title": "Router 2", "password": ""}, 200).Body, &it)
	if it["has_password"] != false {
		t.Fatalf("cleared = %v", it)
	}

	c.must("DELETE", "/api/vault/"+id, nil, 200)
	c.must("DELETE", "/api/vault/"+id, nil, 404)
	if l.audits(t, "vault.item_created") != 1 || l.audits(t, "vault.revealed") != 2 {
		t.Fatal("audit missing")
	}

	anon := newClient(t, l.env, "192.168.1.30")
	anon.must("GET", "/api/vault", nil, 401)
}
