// Package apicall holds what the generated product packages (kyc,
// creditrisk) share: the transport that turns non-2xx responses into
// *creditsafe.APIError, and the helpers generated methods use to return a
// response's body. The templates in codegen/templates call these.
package apicall

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/scriplylabs/goclient-creditsafe"
)

// CheckingDoer sends requests through a creditsafe.Client and returns every
// non-2xx response as a *creditsafe.APIError, so generated methods only see
// successful responses.
type CheckingDoer struct {
	Client *creditsafe.Client
}

// Do sends req and checks the response's status.
func (d CheckingDoer) Do(req *http.Request) (*http.Response, error) {
	resp, err := d.Client.Do(req)
	if err != nil {
		return nil, err
	}

	if err := creditsafe.CheckResponse(resp); err != nil {
		return nil, err
	}

	return resp, nil
}

// BuildServerURL returns the base URL of cs with the trailing slash generated
// clients expect.
func BuildServerURL(cs *creditsafe.Client) string {
	return strings.TrimSuffix(cs.BaseURL(), "/") + "/"
}

// Decode reads a 2xx JSON response into a new T. It returns nil, nil for a
// response with no body (204 No Content), which only operations whose spec
// allows one can receive.
func Decode[T any](op string, resp *http.Response, err error) (*T, error) {
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("creditsafe: %s: read response: %w", op, err)
	}

	if resp.StatusCode == http.StatusNoContent || len(strings.TrimSpace(string(body))) == 0 {
		return nil, nil
	}

	out := new(T)
	if err := json.Unmarshal(body, out); err != nil {
		return nil, fmt.Errorf("creditsafe: %s: decode %d response: %w", op, resp.StatusCode, err)
	}

	return out, nil
}

// DecodeValue is Decode for response types returned by value (slices).
func DecodeValue[T any](op string, resp *http.Response, err error) (T, error) {
	var zero T

	out, err := Decode[T](op, resp, err)
	if err != nil || out == nil {
		return zero, err
	}

	return *out, nil
}

// Discard reads and closes the body of a response that carries no result.
func Discard(resp *http.Response, err error) error {
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	_, err = io.Copy(io.Discard, resp.Body)

	return err
}
