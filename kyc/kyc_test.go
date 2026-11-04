package kyc

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/scriplylabs/goclient-creditsafe"
)

// recorded is one request the fake API received.
type recorded struct {
	method, path, query, auth, contentType, body string
}

// newFakeKYC serves handler under /v1 and returns a Client pointed at it, with
// a static token so no login happens.
func newFakeKYC(t *testing.T, handler http.HandlerFunc) (*Client, *[]recorded) {
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

func TestGetProfileDecodes(t *testing.T) {
	k, reqs := newFakeKYC(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, `{"id":"7b0f8a52-3c38-4a8e-9c49-2f1d1f6a8e10","assignedTo":"Ada"}`)
	})

	profile, err := k.GetProfile(context.Background(), "p-1")
	if err != nil {
		t.Fatal(err)
	}

	if profile.AssignedTo == nil || *profile.AssignedTo != "Ada" || profile.Id == nil {
		t.Errorf("profile = %+v", profile)
	}

	got := (*reqs)[0]
	if got.method != http.MethodGet || got.path != "/v1/compliance/kyc-protect/profiles/p-1" || got.auth != "Bearer tok" {
		t.Errorf("request = %+v", got)
	}
}

func TestGetProfileNotFoundIsAPIError(t *testing.T) {
	k, _ := newFakeKYC(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusNotFound, `{"correlationId":"c-9","message":"Profile not found"}`)
	})

	profile, err := k.GetProfile(context.Background(), "missing")
	if profile != nil {
		t.Errorf("profile = %+v, want nil", profile)
	}

	if !errors.Is(err, creditsafe.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}

	var apiErr *creditsafe.APIError
	if !errors.As(err, &apiErr) || apiErr.CorrelationID != "c-9" || apiErr.Message != "Profile not found" {
		t.Errorf("APIError = %+v", apiErr)
	}
}

func TestDeleteProfileNoContent(t *testing.T) {
	k, reqs := newFakeKYC(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	if err := k.DeleteProfile(context.Background(), "p-1"); err != nil {
		t.Fatal(err)
	}

	if got := (*reqs)[0]; got.method != http.MethodDelete || got.path != "/v1/compliance/kyc-protect/profiles/p-1" {
		t.Errorf("request = %+v", got)
	}
}

func TestDeleteProfileForbidden(t *testing.T) {
	k, _ := newFakeKYC(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusForbidden, `{"message":"Access forbidden"}`)
	})

	if err := k.DeleteProfile(context.Background(), "p-1"); !errors.Is(err, creditsafe.ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
}

func TestListProfilesSendsQuery(t *testing.T) {
	k, reqs := newFakeKYC(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, `{}`)
	})

	term := "acme"
	if _, err := k.ListProfiles(context.Background(), &ListProfilesParams{SearchTerm: &term}); err != nil {
		t.Fatal(err)
	}

	if got := (*reqs)[0]; got.query != "searchTerm=acme" {
		t.Errorf("query = %q", got.query)
	}
}

func TestListProfileAddressesReturnsSlice(t *testing.T) {
	k, _ := newFakeKYC(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, `[{},{}]`)
	})

	addresses, err := k.ListProfileAddresses(context.Background(), openapi_types.UUID{})
	if err != nil {
		t.Fatal(err)
	}

	if len(addresses) != 2 {
		t.Errorf("len = %d, want 2", len(addresses))
	}
}

func TestCreateProfileSendsJSON(t *testing.T) {
	k, reqs := newFakeKYC(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusCreated, `{}`)
	})

	if _, err := k.CreateProfile(context.Background(), CreateProfileJSONRequestBody{}); err != nil {
		t.Fatal(err)
	}

	got := (*reqs)[0]
	if got.method != http.MethodPost || got.path != "/v1/compliance/kyc-protect/profiles" || got.contentType != "application/json" || got.body == "" {
		t.Errorf("request = %+v", got)
	}
}

func TestMalformedSuccessBody(t *testing.T) {
	k, _ := newFakeKYC(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, `not json`)
	})

	_, err := k.GetProfile(context.Background(), "p-1")

	var apiErr *creditsafe.APIError
	if err == nil || errors.As(err, &apiErr) {
		t.Fatalf("err = %v, want a decode error (not an APIError)", err)
	}
}
