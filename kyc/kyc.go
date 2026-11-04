// Package kyc is a client for the Creditsafe KYC Protect API: profiles, AML
// screening of businesses and individuals, monitoring, reports and RBAC.
//
// This package is unofficial and not affiliated with or endorsed by
// Creditsafe. Its types and methods are generated from Creditsafe's OpenAPI
// spec (openapi/kycProtect.json) by scripts/generate.sh. Edit the spec, the
// overlay (openapi/overlay.yaml, which names the methods) or the templates,
// never kyc.gen.go.
//
//	cs, err := creditsafe.New(creditsafe.WithCredentials(user, pass), creditsafe.WithSandbox())
//	if err != nil { ... }
//	k := kyc.New(cs)
//	profile, err := k.GetProfile(ctx, profileID)
//	if errors.Is(err, creditsafe.ErrNotFound) { ... }
//
// Every method returns a *creditsafe.APIError for a non-2xx response; match
// it with errors.Is against the creditsafe sentinels.
package kyc

//go:generate ../scripts/generate.sh

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/scriplylabs/goclient-creditsafe"
)

// Client calls the KYC Protect API. It is safe for concurrent use.
type Client struct {
	raw *rawClient
}

// New returns a KYC Protect client that sends its requests through cs, which
// handles login, token renewal and errors.
func New(cs *creditsafe.Client) *Client {
	return &Client{raw: &rawClient{
		Server: strings.TrimSuffix(cs.BaseURL(), "/") + "/",
		Client: checkingDoer{cs: cs},
	}}
}

// checkingDoer sends requests through the creditsafe client and turns every
// non-2xx response into a *creditsafe.APIError, so generated methods only see
// successful responses.
type checkingDoer struct {
	cs *creditsafe.Client
}

func (d checkingDoer) Do(req *http.Request) (*http.Response, error) {
	resp, err := d.cs.Do(req)
	if err != nil {
		return nil, err
	}

	if err := creditsafe.CheckResponse(resp); err != nil {
		return nil, err
	}

	return resp, nil
}

// decode reads a 2xx JSON response into a new T. It returns nil, nil for a
// response with no body (204 No Content), which only operations whose spec
// allows one can receive.
func decode[T any](op string, resp *http.Response, err error) (*T, error) {
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("kyc: %s: read response: %w", op, err)
	}

	if resp.StatusCode == http.StatusNoContent || len(strings.TrimSpace(string(body))) == 0 {
		return nil, nil
	}

	out := new(T)
	if err := json.Unmarshal(body, out); err != nil {
		return nil, fmt.Errorf("kyc: %s: decode %d response: %w", op, resp.StatusCode, err)
	}

	return out, nil
}

// decodeValue is decode for response types returned by value (slices).
func decodeValue[T any](op string, resp *http.Response, err error) (T, error) {
	var zero T

	out, err := decode[T](op, resp, err)
	if err != nil || out == nil {
		return zero, err
	}

	return *out, nil
}

// discard reads and closes the body of a response that carries no result.
func discard(resp *http.Response, err error) error {
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	_, err = io.Copy(io.Discard, resp.Body)

	return err
}
