package creditrisk

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/scriplylabs/goclient-creditsafe"
)

// recorded is one request the fake API received.
type recorded struct {
	method, path, query, auth, contentType, body string
}

// newFakeCreditRisk serves handler under /v1 and returns a Client pointed at
// it, with a static token so no login happens.
func newFakeCreditRisk(t *testing.T, handler http.HandlerFunc) (*Client, *[]recorded) {
	t.Helper()

	var reqs []recorded

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		reqs = append(reqs, recorded{r.Method, r.URL.Path, r.URL.RawQuery, r.Header.Get("Authorization"), r.Header.Get("Content-Type"), string(body)})
		handler(w, r)
	}))
	t.Cleanup(srv.Close)

	cs, err := creditsafe.New(creditsafe.WithToken("tok"), creditsafe.WithBaseURL(srv.URL+"/v1"))
	if err != nil {
		t.Fatal(err)
	}

	return New(cs), &reqs
}

func writeJSON(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

func TestGetCompanySearchCriteria(t *testing.T) {
	cr, reqs := newFakeCreditRisk(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, `{"correlationId":"c-1","languages":["EN"],"criteriaSets":[{},{}]}`)
	})

	criteria, err := cr.GetCompanySearchCriteria(context.Background(), &GetCompanySearchCriteriaParams{Countries: "US"})
	if err != nil {
		t.Fatal(err)
	}

	if criteria.CriteriaSets == nil || len(*criteria.CriteriaSets) != 2 || criteria.Languages == nil || (*criteria.Languages)[0] != "EN" {
		t.Errorf("criteria = %+v", criteria)
	}

	got := (*reqs)[0]
	if got.method != http.MethodGet || got.path != "/v1/companies/searchcriteria" || got.query != "countries=US" || got.auth != "Bearer tok" {
		t.Errorf("request = %+v", got)
	}
}

func TestSearchCompaniesQuery(t *testing.T) {
	cr, reqs := newFakeCreditRisk(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, `{}`)
	})

	regNo := "12345"
	if _, err := cr.SearchCompanies(context.Background(), &SearchCompaniesParams{Countries: "GB", RegNo: &regNo}); err != nil {
		t.Fatal(err)
	}

	if got := (*reqs)[0]; got.path != "/v1/companies" || !strings.Contains(got.query, "countries=GB") || !strings.Contains(got.query, "regNo=12345") {
		t.Errorf("request = %+v", got)
	}
}

func TestGetCompanyCreditReportNotFound(t *testing.T) {
	cr, reqs := newFakeCreditRisk(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusNotFound, `{"correlationId":"c-2","message":"Company not found"}`)
	})

	report, err := cr.GetCompanyCreditReport(context.Background(), "US-X-1", nil)
	if report != nil || !errors.Is(err, creditsafe.ErrNotFound) {
		t.Fatalf("report = %v, err = %v; want nil, ErrNotFound", report, err)
	}

	if got := (*reqs)[0]; got.path != "/v1/companies/US-X-1" {
		t.Errorf("path = %s", got.path)
	}
}

func TestDownloadFreshInvestigationAttachmentReturnsFile(t *testing.T) {
	cr, reqs := newFakeCreditRisk(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		_, _ = io.WriteString(w, "%PDF-1.7 fake")
	})

	resp, err := cr.DownloadFreshInvestigationAttachment(context.Background(), "o-1", "a-1")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}

	if string(body) != "%PDF-1.7 fake" || resp.Header.Get("Content-Type") != "application/pdf" {
		t.Errorf("got %q (%s)", body, resp.Header.Get("Content-Type"))
	}

	if got := (*reqs)[0]; got.path != "/v1/freshinvestigations/o-1/attachments/a-1" {
		t.Errorf("path = %s", got.path)
	}
}

func TestDownloadFreshInvestigationAttachmentError(t *testing.T) {
	cr, _ := newFakeCreditRisk(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusForbidden, `{"correlationId":"c-3","message":"Forbidden","details":"No access"}`)
	})

	resp, err := cr.DownloadFreshInvestigationAttachment(context.Background(), "o-1", "a-1")
	if resp != nil || !errors.Is(err, creditsafe.ErrForbidden) {
		t.Fatalf("resp = %v, err = %v; want nil, ErrForbidden", resp, err)
	}
}

func TestCreateFreshInvestigationAttachmentSendsBodyAsIs(t *testing.T) {
	cr, reqs := newFakeCreditRisk(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, `{}`)
	})

	const contentType = "multipart/form-data; boundary=xyz"

	body := "--xyz\r\nContent-Disposition: form-data; name=\"file\"; filename=\"a.txt\"\r\n\r\nhi\r\n--xyz--\r\n"
	if _, err := cr.CreateFreshInvestigationAttachment(context.Background(), "o-1", contentType, strings.NewReader(body)); err != nil {
		t.Fatal(err)
	}

	got := (*reqs)[0]
	if got.method != http.MethodPost || got.path != "/v1/freshinvestigations/o-1/attachments" || got.contentType != contentType || got.body != body {
		t.Errorf("request = %+v", got)
	}
}
