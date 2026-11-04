package creditsafe

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Sentinel errors. Every error the API returns is an *APIError, which matches
// one of these with errors.Is according to its status code:
//
//	_, err := kycClient.GetProfile(ctx, id)
//	if errors.Is(err, creditsafe.ErrNotFound) { ... }
//
// Use errors.As to reach the *APIError for its correlation ID and message.
var (
	ErrBadRequest   = errors.New("creditsafe: bad request")         // 400
	ErrUnauthorized = errors.New("creditsafe: unauthorized")        // 401
	ErrForbidden    = errors.New("creditsafe: forbidden")           // 403
	ErrNotFound     = errors.New("creditsafe: not found")           // 404
	ErrConflict     = errors.New("creditsafe: conflict")            // 409
	ErrRateLimited  = errors.New("creditsafe: rate limited")        // 429
	ErrServer       = errors.New("creditsafe: server error")        // 5xx
	ErrUnexpected   = errors.New("creditsafe: unexpected response") // any other non-2xx status
	ErrNoCredential = errors.New("creditsafe: no credentials set")  // client built without WithCredentials or WithToken
)

// ErrAuthentication wraps a failed POST /authenticate (wrong username or
// password, or Creditsafe refusing the login). It is returned alongside the
// *APIError, so errors.Is(err, ErrAuthentication) tells a bad login apart from
// an expired or rejected token, which matches only ErrUnauthorized.
var ErrAuthentication = errors.New("creditsafe: authentication failed")

// maxErrorBody caps how much of an error response is kept in APIError.Body.
const maxErrorBody = 64 << 10

// APIError is a non-2xx response from the Creditsafe API.
//
// Creditsafe returns several error shapes: {correlationId, message, details}
// for most 4xx and 5xx responses, {error} for an invalid or expired token,
// {message} for 403 and failed logins, and RFC 7807 problem details
// (type, title, status, detail, instance) on some endpoints. APIError holds
// the fields of all of them; whichever the response didn't send are empty.
type APIError struct {
	// StatusCode is the HTTP status of the response.
	StatusCode int
	// Method and URL identify the request that failed.
	Method string
	URL    string

	// CorrelationID is Creditsafe's ID for the request; quote it to their support.
	CorrelationID string
	// Message is the short reason. For a token error ({"error": "..."}) it holds that text.
	Message string
	// Details says why the request was rejected, when Creditsafe gives a reason.
	Details string

	// RFC 7807 problem details, when the response used that shape.
	Type     string
	Title    string
	Detail   string
	Instance string

	// RetryAfter is the wait a 429 or 503 asked for in its Retry-After header, or zero.
	RetryAfter time.Duration

	// Body is the raw response body (up to 64 KiB), for shapes this type doesn't decode.
	Body []byte
}

// Error implements error.
func (e *APIError) Error() string {
	var b strings.Builder

	fmt.Fprintf(&b, "creditsafe: %s %s: %d %s", e.Method, e.URL, e.StatusCode, http.StatusText(e.StatusCode))

	for _, part := range []string{e.Message, e.Title, e.Details, e.Detail} {
		if part != "" {
			b.WriteString(": ")
			b.WriteString(part)
		}
	}

	if e.CorrelationID != "" {
		fmt.Fprintf(&b, " (correlation ID %s)", e.CorrelationID)
	}

	return b.String()
}

// Is reports whether the error matches a sentinel for its status code.
func (e *APIError) Is(target error) bool {
	return target == statusSentinel(e.StatusCode)
}

func statusSentinel(status int) error {
	switch {
	case status == http.StatusBadRequest:
		return ErrBadRequest
	case status == http.StatusUnauthorized:
		return ErrUnauthorized
	case status == http.StatusForbidden:
		return ErrForbidden
	case status == http.StatusNotFound:
		return ErrNotFound
	case status == http.StatusConflict:
		return ErrConflict
	case status == http.StatusTooManyRequests:
		return ErrRateLimited
	case status >= 500:
		return ErrServer
	default:
		return ErrUnexpected
	}
}

// errorBody is the union of every error shape in the KYC Protect spec.
type errorBody struct {
	CorrelationID string `json:"correlationId"`
	Message       string `json:"message"`
	Details       string `json:"details"`
	Error         string `json:"error"`
	Type          string `json:"type"`
	Title         string `json:"title"`
	Detail        string `json:"detail"`
	Instance      string `json:"instance"`
}

// CheckResponse returns nil for a 2xx response and an *APIError otherwise.
// For a non-2xx response it reads and closes the body.
func CheckResponse(resp *http.Response) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	defer resp.Body.Close()

	apiErr := &APIError{StatusCode: resp.StatusCode}
	if resp.Request != nil {
		apiErr.Method = resp.Request.Method
		apiErr.URL = resp.Request.URL.Redacted()
	}

	apiErr.RetryAfter = parseRetryAfter(resp.Header.Get("Retry-After"), time.Now())

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
	if err != nil {
		// The status alone still classifies the error; keep what was read.
		apiErr.Body = body
		return apiErr
	}

	apiErr.Body = body

	var decoded errorBody
	if json.Unmarshal(body, &decoded) == nil {
		apiErr.CorrelationID = decoded.CorrelationID
		apiErr.Message = decoded.Message
		apiErr.Details = decoded.Details
		apiErr.Type = decoded.Type
		apiErr.Title = decoded.Title
		apiErr.Detail = decoded.Detail
		apiErr.Instance = decoded.Instance

		if apiErr.Message == "" {
			apiErr.Message = decoded.Error
		}
	} else if text := strings.TrimSpace(string(body)); text != "" && len(text) <= 512 {
		// Not JSON, e.g. a gateway's plain-text or HTML page; keep short ones readable.
		apiErr.Message = text
	}

	return apiErr
}

// parseRetryAfter reads a Retry-After header given in seconds or as an HTTP date.
func parseRetryAfter(value string, now time.Time) time.Duration {
	if value == "" {
		return 0
	}

	if secs, err := strconv.Atoi(value); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second
	}

	if at, err := http.ParseTime(value); err == nil && at.After(now) {
		return at.Sub(now)
	}

	return 0
}
