// Package creditsafe is an unofficial Go client for the Creditsafe Connect
// APIs. It is not affiliated with or endorsed by Creditsafe.
//
// This package holds what every Creditsafe API shares: the Client, which
// logs in and keeps a valid token, and the error types. Each product's
// endpoints live in their own package, built on a Client:
//
//	cs, err := creditsafe.New(creditsafe.WithCredentials(user, pass), creditsafe.WithSandbox())
//	profiles := kyc.New(cs)
package creditsafe

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Base URLs of the Creditsafe Connect API, from the KYC Protect spec.
const (
	ProductionBaseURL = "https://connect.creditsafe.com/v1"
	SandboxBaseURL    = "https://connect.sandbox.creditsafe.com/v1"
)

const userAgent = "goclient-creditsafe"

// Client sends authenticated requests to the Creditsafe Connect API. It is
// safe for concurrent use; share one per set of credentials.
type Client struct {
	baseURL    *url.URL
	httpClient *http.Client
	auth       *authenticator
}

// Option configures a Client.
type Option func(*Client) error

// WithCredentials makes the client log in with a Creditsafe username and
// password. It logs in on the first request, logs in again shortly before the
// token expires, and once more if a request is refused with 401.
func WithCredentials(username, password string) Option {
	return func(c *Client) error {
		if username == "" || password == "" {
			return errors.New("creditsafe: WithCredentials needs a username and a password")
		}

		c.auth.username = username
		c.auth.password = password

		return nil
	}
}

// WithToken makes the client send a token you already have. Without
// WithCredentials too, the client can't renew it: once it expires, requests
// fail with ErrUnauthorized.
func WithToken(token string) Option {
	return func(c *Client) error {
		if token == "" {
			return errors.New("creditsafe: WithToken needs a token")
		}

		c.auth.setToken(token)

		return nil
	}
}

// WithSandbox sends requests to SandboxBaseURL.
func WithSandbox() Option {
	return WithBaseURL(SandboxBaseURL)
}

// WithBaseURL sends requests to baseURL (with its "/v1"), for example a test server.
func WithBaseURL(baseURL string) Option {
	return func(c *Client) error {
		u, err := url.Parse(strings.TrimSuffix(baseURL, "/"))
		if err != nil {
			return fmt.Errorf("creditsafe: parse base URL: %w", err)
		}

		if u.Scheme == "" || u.Host == "" {
			return fmt.Errorf("creditsafe: base URL %q needs a scheme and host", baseURL)
		}

		c.baseURL = u

		return nil
	}
}

// WithHTTPClient sends requests through hc instead of a default client with
// a 60-second timeout.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) error {
		if hc == nil {
			return errors.New("creditsafe: WithHTTPClient needs a client")
		}

		c.httpClient = hc

		return nil
	}
}

// New returns a Client for ProductionBaseURL, unless WithSandbox or
// WithBaseURL says otherwise. It needs WithCredentials or WithToken.
func New(opts ...Option) (*Client, error) {
	c := &Client{
		httpClient: &http.Client{Timeout: 60 * time.Second},
		auth:       &authenticator{},
	}

	if err := WithBaseURL(ProductionBaseURL)(c); err != nil {
		return nil, err
	}

	for _, opt := range opts {
		if err := opt(c); err != nil {
			return nil, err
		}
	}

	if !c.auth.canLogin() && c.auth.token == "" {
		return nil, ErrNoCredential
	}

	c.auth.client = c

	return c, nil
}

// BaseURL returns the URL requests are sent under, e.g. SandboxBaseURL.
func (c *Client) BaseURL() string {
	return c.baseURL.String()
}

// Do sends req with a valid bearer token and returns the raw response, which
// may have any status: check it with CheckResponse. A relative req.URL is
// resolved against the base URL. If the API answers 401 and the client has
// credentials, Do logs in again and resends req once, provided its body can
// be replayed (req.GetBody set, as http.NewRequest does for in-memory bodies).
//
// Do satisfies the HttpRequestDoer interface that generated clients use.
func (c *Client) Do(req *http.Request) (*http.Response, error) {
	ctx := req.Context()

	if !req.URL.IsAbs() {
		req.URL = c.resolve(req.URL)
	}

	token, err := c.auth.validToken(ctx)
	if err != nil {
		return nil, err
	}

	resp, err := c.send(req, token)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusUnauthorized || !c.auth.canLogin() {
		return resp, nil
	}

	retry, err := replayable(req)
	if err != nil || retry == nil {
		return resp, nil //nolint:nilerr // can't resend; the caller gets the 401
	}

	// The token was refused before it expired (revoked, or the clock is off):
	// drop it, log in again, and resend once.
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	token, err = c.auth.renew(ctx, token)
	if err != nil {
		return nil, err
	}

	return c.send(retry, token)
}

// DoJSON sends a JSON request to path (relative to the base URL, e.g.
// "/compliance/kyc-protect/profiles") with query parameters, encoding in as
// the body when it isn't nil and decoding a 2xx response into out when out
// isn't nil. A non-2xx response returns an *APIError.
func (c *Client) DoJSON(ctx context.Context, method, path string, query url.Values, in, out any) error {
	req, err := c.NewRequest(ctx, method, path, query, in)
	if err != nil {
		return err
	}

	resp, err := c.Do(req)
	if err != nil {
		return err
	}

	if err := CheckResponse(resp); err != nil {
		return err
	}

	defer resp.Body.Close()

	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}

	if resp.StatusCode == http.StatusNoContent {
		return nil
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("creditsafe: decode %s %s response: %w", method, path, err)
	}

	return nil
}

// NewRequest builds a request to path (relative to the base URL), with in
// encoded as a JSON body when it isn't nil.
func (c *Client) NewRequest(ctx context.Context, method, path string, query url.Values, in any) (*http.Request, error) {
	u := c.resolve(&url.URL{Path: path, RawQuery: query.Encode()})

	var body io.Reader

	if in != nil {
		encoded, err := json.Marshal(in)
		if err != nil {
			return nil, fmt.Errorf("creditsafe: encode %s %s request: %w", method, path, err)
		}

		body = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, fmt.Errorf("creditsafe: build %s %s request: %w", method, path, err)
	}

	req.Header.Set("Accept", "application/json")

	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	return req, nil
}

// resolve joins a relative URL onto the base URL, keeping the base's "/v1".
func (c *Client) resolve(rel *url.URL) *url.URL {
	u := *c.baseURL
	u.Path = strings.TrimSuffix(u.Path, "/") + "/" + strings.TrimPrefix(rel.Path, "/")
	u.RawPath = ""
	u.RawQuery = rel.RawQuery

	return &u
}

// send adds the token and user agent and sends req without retrying.
func (c *Client) send(req *http.Request, token string) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+token)

	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", userAgent)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("creditsafe: %s %s: %w", req.Method, req.URL.Redacted(), err)
	}

	return resp, nil
}

// replayable returns a copy of req with a fresh body, or nil if its body
// can't be read again.
func replayable(req *http.Request) (*http.Request, error) {
	if req.Body == nil || req.Body == http.NoBody {
		return req.Clone(req.Context()), nil
	}

	if req.GetBody == nil {
		return nil, nil
	}

	body, err := req.GetBody()
	if err != nil {
		return nil, err
	}

	retry := req.Clone(req.Context())
	retry.Body = body

	return retry, nil
}
