// Package livetest sets up the live tests (build tag "live") that run against
// the real Creditsafe API with credentials from the repo's gitignored .env
// (see .env.example). It never logs tokens or response contents.
package livetest

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/scriplylabs/goclient-creditsafe"
)

// LoadDotEnv sets the variables in the repo root's .env that aren't already
// set, for the duration of t. A missing .env is fine.
func LoadDotEnv(t *testing.T) {
	t.Helper()

	path, err := findDotEnv()
	if err != nil {
		return
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}

		key = strings.TrimSpace(strings.TrimPrefix(key, "export "))
		value = strings.Trim(strings.TrimSpace(value), `"'`)

		if _, set := os.LookupEnv(key); !set {
			t.Setenv(key, value)
		}
	}

	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
}

// findDotEnv looks for .env in the working directory and its parents, up to
// the directory holding go.mod.
func findDotEnv() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, ".env")); err == nil {
			return filepath.Join(dir, ".env"), nil
		}

		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return "", os.ErrNotExist
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", os.ErrNotExist
		}

		dir = parent
	}
}

// NewClient returns a client using CREDITSAFE_USERNAME and
// CREDITSAFE_PASSWORD, or else CREDITSAFE_TOKEN, from the environment or
// .env. It skips t when neither is set or the token has expired. It uses
// production unless CREDITSAFE_SANDBOX=1.
func NewClient(t *testing.T) *creditsafe.Client {
	t.Helper()
	LoadDotEnv(t)

	user, pass := os.Getenv("CREDITSAFE_USERNAME"), os.Getenv("CREDITSAFE_PASSWORD")
	token := os.Getenv("CREDITSAFE_TOKEN")

	var opts []creditsafe.Option

	switch {
	case user != "" && pass != "":
		opts = append(opts, creditsafe.WithCredentials(user, pass))
	case token != "":
		// A token from elsewhere can't be renewed; it lasts about an hour.
		if exp := ReadTokenExpiry(token); !exp.IsZero() && time.Now().After(exp) {
			t.Skipf("CREDITSAFE_TOKEN expired at %s; get a fresh one", exp.Format(time.RFC3339))
		}

		opts = append(opts, creditsafe.WithToken(token))
	default:
		t.Skip("set CREDITSAFE_USERNAME and CREDITSAFE_PASSWORD, or CREDITSAFE_TOKEN, in .env")
	}

	if os.Getenv("CREDITSAFE_SANDBOX") == "1" {
		opts = append(opts, creditsafe.WithSandbox())
	}

	cs, err := creditsafe.New(opts...)
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("base URL %s", cs.BaseURL())

	return cs
}

// LogAPIError logs the fields of err's *APIError and the top-level keys of
// its body (not the body, which may echo request data), to compare with the
// error shapes in the spec.
func LogAPIError(t *testing.T, err error) {
	t.Helper()

	var apiErr *creditsafe.APIError
	if !errors.As(err, &apiErr) {
		t.Logf("not an APIError: %T %v", err, err)
		return
	}

	t.Logf("status=%d correlationId=%q message=%q details=%q type=%q title=%q detail=%q retryAfter=%v bodyKeys=%v",
		apiErr.StatusCode, apiErr.CorrelationID, apiErr.Message, apiErr.Details,
		apiErr.Type, apiErr.Title, apiErr.Detail, apiErr.RetryAfter, ListJSONKeys(apiErr.Body))
}

// ListJSONKeys lists the sorted top-level keys of a JSON object, to show its
// shape without its values.
func ListJSONKeys(body []byte) []string {
	var obj map[string]json.RawMessage
	if json.Unmarshal(body, &obj) != nil {
		return []string{"<not a JSON object>"}
	}

	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	return keys
}

// ReadTokenExpiry reads a JWT's exp claim, or returns the zero time.
func ReadTokenExpiry(token string) time.Time {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return time.Time{}
	}

	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return time.Time{}
	}

	var claims struct {
		Exp int64 `json:"exp"`
	}

	if json.Unmarshal(payload, &claims) != nil || claims.Exp == 0 {
		return time.Time{}
	}

	return time.Unix(claims.Exp, 0)
}
