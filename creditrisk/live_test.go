//go:build live

// Live test against the real Credit and Risk API, behind the "live" build tag
// so go test and CI never run it. It makes one call, to an endpoint that
// orders nothing (search criteria); keep it that way: this is production, and
// reports and images may be billed. Credentials come from .env (see
// .env.example and internal/livetest):
//
//	go test -tags live -run Live -v ./creditrisk
package creditrisk

import (
	"context"
	"testing"

	"github.com/scriplylabs/goclient-creditsafe/internal/livetest"
)

func TestLiveGetCompanySearchCriteria(t *testing.T) {
	cr := New(livetest.NewClient(t))

	criteria, err := cr.GetCompanySearchCriteria(context.Background(), &GetCompanySearchCriteriaParams{Countries: "US"})
	if err != nil {
		livetest.LogAPIError(t, err)
		t.Fatal(err)
	}

	sets := 0
	if criteria.CriteriaSets != nil {
		sets = len(*criteria.CriteriaSets)
	}

	if sets == 0 {
		t.Error("no criteria sets for US; did the response decode?")
	}

	t.Logf("GetCompanySearchCriteria(US) ok: %d criteria sets", sets)
}
