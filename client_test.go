package creditsafe

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// makeJWT returns an unsigned JWT whose exp claim is exp.
func makeJWT(t *testing.T, id string, exp time.Time) string {
	t.Helper()

	enc := base64.RawURLEncoding
	header := enc.EncodeToString([]byte(`{"alg":"none"}`))
	payload := enc.EncodeToString(fmt.Appendf(nil, `{"exp":%d,"jti":%q}`, exp.Unix(), id))

	return header + "." + payload + ".sig"
}

// fakeAPI is a minimal Creditsafe: /authenticate hands out tokens, and
// /echo answers with the token and body it received.
type fakeAPI struct {
	t        *testing.T
	logins   atomic.Int32
	expiry   time.Duration
	rejectOK func(token string) bool // when set and false, /echo answers 401
	mu       sync.Mutex
	bodies   []string
}

func (f *fakeAPI) handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /v1/authenticate", func(w http.ResponseWriter, r *http.Request) {
		var req AuthenticateRequest
		_ = json.NewDecoder(r.Body).Decode(&req)

		if req.Username != "user" || req.Password != "pass" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"message":"Access denied -  Please check that your username and password are correct."}`)

			return
		}

		n := f.logins.Add(1)
		_ = json.NewEncoder(w).Encode(AuthenticateResponse{Token: makeJWT(f.t, fmt.Sprint(n), now().Add(f.expiry))})
	})

	mux.HandleFunc("/v1/echo", func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		body, _ := io.ReadAll(r.Body)

		f.mu.Lock()
		f.bodies = append(f.bodies, string(body))
		f.mu.Unlock()

		if f.rejectOK != nil && !f.rejectOK(token) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"error":"Invalid token"}`)

			return
		}

		_ = json.NewEncoder(w).Encode(map[string]string{"token": token, "body": string(body), "query": r.URL.RawQuery})
	})

	return mux
}

func newFake(t *testing.T) (*fakeAPI, *httptest.Server) {
	t.Helper()

	f := &fakeAPI{t: t, expiry: time.Hour}
	srv := httptest.NewServer(f.handler())
	t.Cleanup(srv.Close)

	return f, srv
}

func TestNewNeedsCredentials(t *testing.T) {
	if _, err := New(); !errors.Is(err, ErrNoCredential) {
		t.Fatalf("New() error = %v, want ErrNoCredential", err)
	}

	if _, err := New(WithCredentials("user", "")); err == nil {
		t.Fatal("WithCredentials with an empty password: want error")
	}

	if _, err := New(WithToken("t"), WithBaseURL("not a url")); err == nil {
		t.Fatal("WithBaseURL without scheme: want error")
	}
}

func TestBaseURLs(t *testing.T) {
	c, err := New(WithToken("t"))
	if err != nil {
		t.Fatal(err)
	}

	if c.BaseURL() != ProductionBaseURL {
		t.Errorf("default BaseURL = %s, want production", c.BaseURL())
	}

	c, err = New(WithToken("t"), WithSandbox())
	if err != nil {
		t.Fatal(err)
	}

	if c.BaseURL() != SandboxBaseURL {
		t.Errorf("sandbox BaseURL = %s", c.BaseURL())
	}
}

func TestLogsInOnceAndSendsBearer(t *testing.T) {
	f, srv := newFake(t)

	c, err := New(WithCredentials("user", "pass"), WithBaseURL(srv.URL+"/v1"))
	if err != nil {
		t.Fatal(err)
	}

	if f.logins.Load() != 0 {
		t.Fatal("New logged in; it should wait for the first request")
	}

	var got map[string]string

	for i := range 3 {
		if err := c.DoJSON(context.Background(), http.MethodPost, "/echo", nil, map[string]int{"n": i}, &got); err != nil {
			t.Fatal(err)
		}
	}

	if f.logins.Load() != 1 {
		t.Errorf("logins = %d, want 1", f.logins.Load())
	}

	if jwtExpiry(got["token"]).IsZero() {
		t.Errorf("echoed token %q isn't the issued JWT", got["token"])
	}

	if got["body"] != `{"n":2}` {
		t.Errorf("body = %q", got["body"])
	}
}

func TestConcurrentRequestsShareOneLogin(t *testing.T) {
	f, srv := newFake(t)

	c, err := New(WithCredentials("user", "pass"), WithBaseURL(srv.URL+"/v1"))
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup

	for range 20 {
		wg.Add(1)

		go func() {
			defer wg.Done()

			if err := c.DoJSON(context.Background(), http.MethodGet, "/echo", nil, nil, nil); err != nil {
				t.Error(err)
			}
		}()
	}

	wg.Wait()

	if f.logins.Load() != 1 {
		t.Errorf("logins = %d, want 1", f.logins.Load())
	}
}

func TestRenewsBeforeExpiry(t *testing.T) {
	f, srv := newFake(t)

	c, err := New(WithCredentials("user", "pass"), WithBaseURL(srv.URL+"/v1"))
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	if err := c.DoJSON(ctx, http.MethodGet, "/echo", nil, nil, nil); err != nil {
		t.Fatal(err)
	}

	// Move the clock to 30 seconds before the token expires: inside renewBefore.
	start := time.Now()
	now = func() time.Time { return start.Add(time.Hour - 30*time.Second) }

	t.Cleanup(func() { now = time.Now })

	if err := c.DoJSON(ctx, http.MethodGet, "/echo", nil, nil, nil); err != nil {
		t.Fatal(err)
	}

	if f.logins.Load() != 2 {
		t.Errorf("logins = %d, want 2", f.logins.Load())
	}
}

func TestRetriesOnceAfter401WithSameBody(t *testing.T) {
	f, srv := newFake(t)

	var first string

	// Refuse the first token ever issued, as if it was revoked.
	f.rejectOK = func(token string) bool {
		if first == "" {
			first = token
		}

		return token != first
	}

	c, err := New(WithCredentials("user", "pass"), WithBaseURL(srv.URL+"/v1"))
	if err != nil {
		t.Fatal(err)
	}

	var got map[string]string
	if err := c.DoJSON(context.Background(), http.MethodPut, "/echo", nil, map[string]string{"a": "b"}, &got); err != nil {
		t.Fatal(err)
	}

	if f.logins.Load() != 2 {
		t.Errorf("logins = %d, want 2", f.logins.Load())
	}

	if len(f.bodies) != 2 || f.bodies[0] != f.bodies[1] || f.bodies[1] != `{"a":"b"}` {
		t.Errorf("bodies = %q, want the same body twice", f.bodies)
	}
}

func TestStaticTokenIsNotRenewed(t *testing.T) {
	f, srv := newFake(t)
	f.rejectOK = func(string) bool { return false }

	c, err := New(WithToken("static"), WithBaseURL(srv.URL+"/v1"))
	if err != nil {
		t.Fatal(err)
	}

	err = c.DoJSON(context.Background(), http.MethodGet, "/echo", nil, nil, nil)
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("err = %v, want ErrUnauthorized", err)
	}

	if errors.Is(err, ErrAuthentication) {
		t.Error("a refused token matched ErrAuthentication; that's for refused logins")
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Message != "Invalid token" {
		t.Errorf("APIError = %+v, want message from the {error} shape", apiErr)
	}

	if f.logins.Load() != 0 {
		t.Errorf("logins = %d, want 0", f.logins.Load())
	}
}

func TestWrongPassword(t *testing.T) {
	_, srv := newFake(t)

	c, err := New(WithCredentials("user", "wrong"), WithBaseURL(srv.URL+"/v1"))
	if err != nil {
		t.Fatal(err)
	}

	err = c.DoJSON(context.Background(), http.MethodGet, "/echo", nil, nil, nil)
	if !errors.Is(err, ErrAuthentication) || !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("err = %v, want ErrAuthentication and ErrUnauthorized", err)
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) || !strings.HasPrefix(apiErr.Message, "Access denied") {
		t.Errorf("APIError = %+v", apiErr)
	}
}

func TestQueryIsSent(t *testing.T) {
	_, srv := newFake(t)

	c, err := New(WithToken("t"), WithBaseURL(srv.URL+"/v1/"))
	if err != nil {
		t.Fatal(err)
	}

	var got map[string]string
	if err := c.DoJSON(context.Background(), http.MethodGet, "echo", map[string][]string{"pageSize": {"10"}}, nil, &got); err != nil {
		t.Fatal(err)
	}

	if got["query"] != "pageSize=10" {
		t.Errorf("query = %q", got["query"])
	}
}
