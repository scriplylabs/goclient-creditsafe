package creditsafe

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func response(status int, body string, header http.Header) *http.Response {
	if header == nil {
		header = http.Header{}
	}

	u, _ := url.Parse("https://connect.sandbox.creditsafe.com/v1/compliance/kyc-protect/profiles/42")

	return &http.Response{
		StatusCode: status,
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    &http.Request{Method: http.MethodGet, URL: u},
	}
}

func TestCheckResponseShapes(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		body     string
		sentinel error
		want     APIError
	}{
		{
			name:     "connect error (400/404/409/500)",
			status:   http.StatusNotFound,
			body:     `{"correlationId":"c-1","message":"Profile not found","details":"No profile with id 42"}`,
			sentinel: ErrNotFound,
			want:     APIError{CorrelationID: "c-1", Message: "Profile not found", Details: "No profile with id 42"},
		},
		{
			name:     "invalid token (401)",
			status:   http.StatusUnauthorized,
			body:     `{"error":"Invalid token"}`,
			sentinel: ErrUnauthorized,
			want:     APIError{Message: "Invalid token"},
		},
		{
			name:     "forbidden (403)",
			status:   http.StatusForbidden,
			body:     `{"message":"Access forbidden"}`,
			sentinel: ErrForbidden,
			want:     APIError{Message: "Access forbidden"},
		},
		{
			name:     "problem details",
			status:   http.StatusConflict,
			body:     `{"type":"about:blank","title":"Conflict","status":409,"detail":"Already linked","instance":"/x"}`,
			sentinel: ErrConflict,
			want:     APIError{Type: "about:blank", Title: "Conflict", Detail: "Already linked", Instance: "/x"},
		},
		{
			name:     "plain text from a gateway",
			status:   http.StatusBadGateway,
			body:     "Bad Gateway",
			sentinel: ErrServer,
			want:     APIError{Message: "Bad Gateway"},
		},
		{
			name:     "bad request",
			status:   http.StatusBadRequest,
			body:     `{"message":"Invalid pageSize"}`,
			sentinel: ErrBadRequest,
			want:     APIError{Message: "Invalid pageSize"},
		},
		{
			name:     "unlisted status",
			status:   http.StatusTeapot,
			body:     ``,
			sentinel: ErrUnexpected,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CheckResponse(response(tt.status, tt.body, nil))

			if !errors.Is(err, tt.sentinel) {
				t.Fatalf("errors.Is(%v, %v) = false", err, tt.sentinel)
			}

			var got *APIError
			if !errors.As(err, &got) {
				t.Fatalf("%v is not an *APIError", err)
			}

			if got.StatusCode != tt.status || got.Method != http.MethodGet || !strings.HasSuffix(got.URL, "/profiles/42") {
				t.Errorf("request fields = %d %s %s", got.StatusCode, got.Method, got.URL)
			}

			if got.CorrelationID != tt.want.CorrelationID || got.Message != tt.want.Message || got.Details != tt.want.Details ||
				got.Type != tt.want.Type || got.Title != tt.want.Title || got.Detail != tt.want.Detail || got.Instance != tt.want.Instance {
				t.Errorf("got %+v\nwant %+v", *got, tt.want)
			}

			if string(got.Body) != tt.body {
				t.Errorf("Body = %q", got.Body)
			}
		})
	}
}

func TestCheckResponseSuccess(t *testing.T) {
	for _, status := range []int{200, 201, 202, 204} {
		if err := CheckResponse(response(status, "{}", nil)); err != nil {
			t.Errorf("status %d: %v", status, err)
		}
	}
}

func TestErrorMessage(t *testing.T) {
	err := CheckResponse(response(http.StatusNotFound, `{"correlationId":"c-1","message":"Profile not found","details":"No profile with id 42"}`, nil))

	want := "creditsafe: GET https://connect.sandbox.creditsafe.com/v1/compliance/kyc-protect/profiles/42: 404 Not Found: Profile not found: No profile with id 42 (correlation ID c-1)"
	if err.Error() != want {
		t.Errorf("Error() =\n%s\nwant\n%s", err.Error(), want)
	}
}

func TestErrorMatchesOnlyItsSentinel(t *testing.T) {
	err := CheckResponse(response(http.StatusNotFound, `{}`, nil))

	for _, other := range []error{ErrBadRequest, ErrUnauthorized, ErrForbidden, ErrConflict, ErrRateLimited, ErrServer, ErrUnexpected, ErrAuthentication} {
		if errors.Is(err, other) {
			t.Errorf("a 404 matched %v", other)
		}
	}
}

func TestRetryAfter(t *testing.T) {
	err := CheckResponse(response(http.StatusTooManyRequests, `{}`, http.Header{"Retry-After": {"30"}}))

	var apiErr *APIError
	if !errors.As(err, &apiErr) || !errors.Is(err, ErrRateLimited) || apiErr.RetryAfter != 30*time.Second {
		t.Fatalf("err = %#v", err)
	}

	start := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	if got := parseRetryAfter(start.Add(time.Minute).Format(http.TimeFormat), start); got != time.Minute {
		t.Errorf("HTTP-date Retry-After = %v, want 1m", got)
	}

	if got := parseRetryAfter("soon", start); got != 0 {
		t.Errorf("bad Retry-After = %v, want 0", got)
	}
}

func TestJWTExpiry(t *testing.T) {
	exp := time.Unix(1595419809, 0)
	if got := jwtExpiry(makeJWT(t, "x", exp)); !got.Equal(exp) {
		t.Errorf("jwtExpiry = %v, want %v", got, exp)
	}

	for _, bad := range []string{"", "opaque-token", "a.!!!.c", "a.e30.c"} {
		if got := jwtExpiry(bad); !got.IsZero() {
			t.Errorf("jwtExpiry(%q) = %v, want zero", bad, got)
		}
	}
}
