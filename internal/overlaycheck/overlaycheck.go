// Package overlaycheck tests that a generated package's OpenAPI overlay gives
// every operation in its spec a Go name.
package overlaycheck

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"
)

// action matches one rename in an overlay: a target line followed by an
// update line and an operationId line, e.g. a target of
// $.paths['/companies'].get with operationId SearchCompanies.
var action = regexp.MustCompile(`(?m)^\s*- target: \$\.paths\['([^']+)'\]\.(\w+)\s*\n\s*update:\s*\n\s*operationId: (\w+)\s*$`)

var methods = map[string]bool{"get": true, "put": true, "post": true, "delete": true, "patch": true, "head": true, "options": true}

// Check fails t unless the overlay at overlayPath names every operation of
// the spec at specPath exactly once, with names used only once. Paths in
// skip (such as "/authenticate", which overlays remove) aren't checked.
func Check(t *testing.T, specPath, overlayPath string, skip ...string) {
	t.Helper()

	specBytes, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatal(err)
	}

	var spec struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}

	if err := json.Unmarshal(specBytes, &spec); err != nil {
		t.Fatal(err)
	}

	skipped := map[string]bool{}
	for _, p := range skip {
		skipped[p] = true
	}

	ops := map[string]bool{}

	for path, item := range spec.Paths {
		if skipped[path] {
			continue
		}

		for method := range item {
			if methods[method] {
				ops[method+" "+path] = true
			}
		}
	}

	overlay, err := os.ReadFile(overlayPath)
	if err != nil {
		t.Fatal(err)
	}

	named := map[string]string{}
	names := map[string]string{}

	for _, m := range action.FindAllStringSubmatch(string(overlay), -1) {
		path, method, name := m[1], m[2], m[3]
		op := method + " " + path

		if !ops[op] {
			t.Errorf("%s names %s, which isn't in the spec", overlayPath, strings.ToUpper(op))
		}

		if prev, ok := named[op]; ok {
			t.Errorf("%s names %s twice: %s and %s", overlayPath, strings.ToUpper(op), prev, name)
		}

		if prev, ok := names[name]; ok {
			t.Errorf("%s uses %s for both %s and %s", overlayPath, name, strings.ToUpper(prev), strings.ToUpper(op))
		}

		named[op] = name
		names[name] = op
	}

	for op := range ops {
		if _, ok := named[op]; !ok {
			t.Errorf("spec operation %s has no name in %s", strings.ToUpper(op), overlayPath)
		}
	}

	if len(named) == 0 {
		t.Fatalf("found no renames in %s; has its layout changed?", overlayPath)
	}
}
