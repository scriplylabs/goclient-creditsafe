package creditsafe

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// renewBefore is how long before a token's exp claim the client logs in again,
// so a request never leaves with a token about to lapse.
const renewBefore = time.Minute

// now is replaced in tests.
var now = time.Now

// AuthenticateRequest is the body of POST /authenticate.
type AuthenticateRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// AuthenticateResponse is the body of a successful POST /authenticate.
type AuthenticateResponse struct {
	// Token is a JWT, sent as "Authorization: Bearer <token>".
	Token string `json:"token"`
}

// authenticator holds the current token and logs in for a new one.
type authenticator struct {
	client   *Client
	username string
	password string

	mu      sync.Mutex
	token   string
	expires time.Time // zero when the token's expiry is unknown
}

func (a *authenticator) canLogin() bool {
	return a.username != "" && a.password != ""
}

// setToken stores token and reads its expiry from the JWT's exp claim.
func (a *authenticator) setToken(token string) {
	a.token = token
	a.expires = jwtExpiry(token)
}

// validToken returns the current token, logging in first when there is none
// or it expires within renewBefore.
func (a *authenticator) validToken(ctx context.Context) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.token != "" && (a.expires.IsZero() || now().Add(renewBefore).Before(a.expires)) {
		return a.token, nil
	}

	if !a.canLogin() {
		if a.token != "" {
			// An expired static token: send it anyway and let the API say 401.
			return a.token, nil
		}

		return "", ErrNoCredential
	}

	return a.loginLocked(ctx)
}

// renew logs in again after the API refused rejected. If another goroutine
// already replaced that token, renew returns the newer one without logging in.
func (a *authenticator) renew(ctx context.Context, rejected string) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.token != "" && a.token != rejected {
		return a.token, nil
	}

	return a.loginLocked(ctx)
}

func (a *authenticator) loginLocked(ctx context.Context) (string, error) {
	resp, err := a.client.Authenticate(ctx, a.username, a.password)
	if err != nil {
		return "", err
	}

	a.setToken(resp.Token)

	return a.token, nil
}

// Authenticate calls POST /authenticate and returns the token. The client
// calls it for you when built WithCredentials; call it yourself only to get a
// token for elsewhere. It doesn't change the client's token.
//
// A refused login returns an error matching both ErrAuthentication and the
// status's sentinel (ErrUnauthorized for a wrong username or password).
func (c *Client) Authenticate(ctx context.Context, username, password string) (*AuthenticateResponse, error) {
	req, err := c.NewRequest(ctx, http.MethodPost, "/authenticate", nil, AuthenticateRequest{Username: username, Password: password})
	if err != nil {
		return nil, err
	}

	// No bearer token here: send directly, not through Do.
	req.Header.Set("User-Agent", userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("creditsafe: POST /authenticate: %w", err)
	}

	if err := CheckResponse(resp); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrAuthentication, err)
	}

	defer resp.Body.Close()

	var out AuthenticateResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("creditsafe: decode POST /authenticate response: %w", err)
	}

	if out.Token == "" {
		return nil, fmt.Errorf("%w: response had no token", ErrAuthentication)
	}

	return &out, nil
}

// jwtExpiry reads the exp claim of a JWT without verifying it (the API does
// that). It returns the zero time when the token isn't a JWT or has no exp.
func jwtExpiry(token string) time.Time {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return time.Time{}
	}

	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return time.Time{}
	}

	var claims struct {
		Exp json.Number `json:"exp"`
	}

	if err := json.Unmarshal(payload, &claims); err != nil || claims.Exp == "" {
		return time.Time{}
	}

	secs, err := claims.Exp.Float64()
	if err != nil || secs <= 0 {
		return time.Time{}
	}

	return time.Unix(int64(secs), 0)
}
