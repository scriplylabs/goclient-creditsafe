package kyc

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"
)

// overlayAction matches one rename in openapi/overlay.yaml, a target line
// followed by an update line and an operationId line, e.g. a target of
// $.paths['/compliance/kyc-protect/profiles'].get with operationId ListProfiles.
var overlayAction = regexp.MustCompile(`(?m)^\s*- target: \$\.paths\['([^']+)'\]\.(\w+)\s*\n\s*update:\s*\n\s*operationId: (\w+)\s*$`)

// TestOverlayNamesEveryOperation checks that every operation in the spec has
// a Go name in the overlay, exactly once, and that no name is used twice.
// When Creditsafe adds an operation to the spec, name it in the overlay.
func TestOverlayNamesEveryOperation(t *testing.T) {
	specBytes, err := os.ReadFile("openapi/kycProtect.json")
	if err != nil {
		t.Fatal(err)
	}

	var spec struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}

	if err := json.Unmarshal(specBytes, &spec); err != nil {
		t.Fatal(err)
	}

	methods := map[string]bool{"get": true, "put": true, "post": true, "delete": true, "patch": true, "head": true, "options": true}
	ops := map[string]bool{}

	for path, item := range spec.Paths {
		if path == "/authenticate" { // removed by the overlay; package creditsafe logs in
			continue
		}

		for method := range item {
			if methods[method] {
				ops[method+" "+path] = true
			}
		}
	}

	overlay, err := os.ReadFile("openapi/overlay.yaml")
	if err != nil {
		t.Fatal(err)
	}

	named := map[string]string{}
	names := map[string]string{}

	for _, m := range overlayAction.FindAllStringSubmatch(string(overlay), -1) {
		path, method, name := m[1], m[2], m[3]
		op := method + " " + path

		if !ops[op] {
			t.Errorf("overlay names %s, which isn't in the spec", strings.ToUpper(op))
		}

		if prev, ok := named[op]; ok {
			t.Errorf("overlay names %s twice: %s and %s", strings.ToUpper(op), prev, name)
		}

		if prev, ok := names[name]; ok {
			t.Errorf("overlay uses %s for both %s and %s", name, strings.ToUpper(prev), strings.ToUpper(op))
		}

		named[op] = name
		names[name] = op
	}

	for op := range ops {
		if _, ok := named[op]; !ok {
			t.Errorf("spec operation %s has no name in openapi/overlay.yaml", strings.ToUpper(op))
		}
	}

	if len(named) == 0 {
		t.Fatal("found no renames in openapi/overlay.yaml; has its layout changed?")
	}
}
