package creditrisk

import (
	"testing"

	"github.com/scriplylabs/goclient-creditsafe/internal/overlaycheck"
)

// TestOverlayNamesEveryOperation fails when the spec has an operation with no
// Go name in openapi/overlay.yaml; name it there and regenerate.
func TestOverlayNamesEveryOperation(t *testing.T) {
	overlaycheck.Check(t, "openapi/creditAndRisk.json", "openapi/overlay.yaml", "/authenticate")
}
