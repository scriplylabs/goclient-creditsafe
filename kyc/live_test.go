//go:build live

// Live test against the real KYC Protect API, behind the "live" build tag so
// go test and CI never run it. It makes one read-only call (one profile);
// keep it that way: this is production. Credentials come from .env (see
// .env.example and internal/livetest):
//
//	go test -tags live -run Live -v ./kyc
package kyc

import (
	"context"
	"errors"
	"testing"

	"github.com/scriplylabs/goclient-creditsafe"
	"github.com/scriplylabs/goclient-creditsafe/internal/livetest"
)

func TestLiveListProfiles(t *testing.T) {
	k := New(livetest.NewClient(t))

	one := 1

	profiles, err := k.ListProfiles(context.Background(), &ListProfilesParams{PageSize: &one})
	if errors.Is(err, creditsafe.ErrForbidden) {
		livetest.LogAPIError(t, err)
		t.Skip("this account doesn't have KYC Protect; Creditsafe enables it per account")
	}

	if err != nil {
		livetest.LogAPIError(t, err)
		t.Fatal(err)
	}

	items := 0
	if profiles.Items != nil {
		items = len(*profiles.Items)
	}

	t.Logf("ListProfiles ok: %d item(s) on the page", items)
}
