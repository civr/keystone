package httpapi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// TestAuditActionsFixture keeps web/src/lib/fixtures/audit-actions.json (whose
// entries the web UI must label in Dutch) equal to the audit actions and login
// methods the Go code writes.
func TestAuditActionsFixture(t *testing.T) {
	var src strings.Builder
	files, _ := filepath.Glob("*.go")
	files = append(files, "../../cmd/keystone-core/main.go")
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		src.Write(b)
	}
	collect := func(res ...*regexp.Regexp) []string {
		set := map[string]bool{}
		for _, re := range res {
			for _, m := range re.FindAllStringSubmatch(src.String(), -1) {
				set[m[1]] = true
			}
		}
		out := make([]string, 0, len(set))
		for k := range set {
			out = append(out, k)
		}
		slices.Sort(out)
		return out
	}
	actions := collect(regexp.MustCompile(`\.Log\(ctx, (?:audit\.Actor\w+|actor\(ctx\)|actorOf\([^)]*\)), "([a-z_.]+)"`))
	methods := collect(
		regexp.MustCompile(`"method": "([a-z_]+)"`),
		regexp.MustCompile(`method :?= "([a-z_]+)"`),
		regexp.MustCompile(`loginFailed\([^)]*, "([a-z_]+)", ip\)`),
	)
	raw, err := os.ReadFile("../../web/src/lib/fixtures/audit-actions.json")
	if err != nil {
		t.Fatal(err)
	}
	var fx struct{ Actions, Methods []string }
	if err := json.Unmarshal(raw, &fx); err != nil {
		t.Fatal(err)
	}
	slices.Sort(fx.Actions)
	slices.Sort(fx.Methods)
	if len(actions) < 20 || !slices.Equal(actions, fx.Actions) {
		t.Errorf("audit actions in Go:\n%q\nin fixture:\n%q", actions, fx.Actions)
	}
	if !slices.Equal(methods, fx.Methods) {
		t.Errorf("login methods in Go:\n%q\nin fixture:\n%q", methods, fx.Methods)
	}
}
