package main

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/civr/keystone/internal/settings"
)

func TestPrintDefaults(t *testing.T) {
	var out bytes.Buffer
	if err := printDefaults(&out); err != nil {
		t.Fatal(err)
	}
	var got settings.Settings
	dec := json.NewDecoder(&out)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&got); err != nil {
		t.Fatalf("output is not Settings JSON: %v", err)
	}
	if !reflect.DeepEqual(got, settings.Defaults()) {
		t.Fatalf("got %+v, want Defaults()", got)
	}
}

// The web UI tests run against a committed copy of the defaults; regenerate it
// with `make web-fixtures` when Settings changes.
func TestWebDefaultsFixtureUpToDate(t *testing.T) {
	var want bytes.Buffer
	if err := printDefaults(&want); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile("../../web/src/lib/fixtures/defaults.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want.Bytes()) {
		t.Fatal("web/src/lib/fixtures/defaults.json is stale: run `make web-fixtures`")
	}
}
