// Package creditrisk is a client for Creditsafe's Credit and Risk API:
// company and director search, company credit reports, director reports,
// company images and fresh investigations.
//
// This package is unofficial and not affiliated with or endorsed by
// Creditsafe. Its types and methods are generated from Creditsafe's OpenAPI
// spec (openapi/creditAndRisk.json) by scripts/generate.sh. Edit the spec,
// the overlay (openapi/overlay.yaml, which names the methods) or the
// templates in codegen/templates, never creditrisk.gen.go.
//
//	cs, err := creditsafe.New(creditsafe.WithCredentials(user, pass))
//	if err != nil { ... }
//	cr := creditrisk.New(cs)
//	criteria, err := cr.GetCompanySearchCriteria(ctx, &creditrisk.GetCompanySearchCriteriaParams{Countries: "US"})
//
// Credit reports, director reports and images are usually billed per call.
//
// Every method returns a *creditsafe.APIError for a non-2xx response; match
// it with errors.Is against the creditsafe sentinels.
package creditrisk

import (
	"github.com/scriplylabs/goclient-creditsafe"
	"github.com/scriplylabs/goclient-creditsafe/internal/apicall"
)

// Client calls the Credit and Risk API. It is safe for concurrent use.
type Client struct {
	raw *rawClient
}

// New returns a Credit and Risk client that sends its requests through cs,
// which handles login, token renewal and errors.
func New(cs *creditsafe.Client) *Client {
	return &Client{raw: &rawClient{
		Server: apicall.BuildServerURL(cs),
		Client: apicall.CheckingDoer{Client: cs},
	}}
}
