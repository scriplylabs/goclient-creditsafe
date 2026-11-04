// Package kyc is a client for the Creditsafe KYC Protect API: profiles, AML
// screening of businesses and individuals, monitoring, reports and RBAC.
//
// This package is unofficial and not affiliated with or endorsed by
// Creditsafe. Its types and methods are generated from Creditsafe's OpenAPI
// spec (openapi/kycProtect.json) by scripts/generate.sh. Edit the spec, the
// overlay (openapi/overlay.yaml, which names the methods) or the templates in
// codegen/templates, never kyc.gen.go.
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

import (
	"github.com/scriplylabs/goclient-creditsafe"
	"github.com/scriplylabs/goclient-creditsafe/internal/apicall"
)

// Client calls the KYC Protect API. It is safe for concurrent use.
type Client struct {
	raw *rawClient
}

// New returns a KYC Protect client that sends its requests through cs, which
// handles login, token renewal and errors.
func New(cs *creditsafe.Client) *Client {
	return &Client{raw: &rawClient{
		Server: apicall.BuildServerURL(cs),
		Client: apicall.CheckingDoer{Client: cs},
	}}
}
