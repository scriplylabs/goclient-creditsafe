# goclient-creditsafe

An unofficial Go client for the [Creditsafe Connect APIs](https://doc.creditsafe.com/).

> **Disclaimer:** This project is not affiliated with, endorsed by, sponsored
> by, or supported by Creditsafe. "Creditsafe" is a trademark of its owner and
> is used here only to say which API this library talks to. For help with
> your Creditsafe account, contact Creditsafe. For problems with this
> library, open an issue here.

## Installation

```sh
go get github.com/scriplylabs/goclient-creditsafe
```

Requires Go 1.24 or later.

## Packages

| Package | API |
| --- | --- |
| `creditsafe` | The shared client: login, token renewal and errors |
| `creditsafe/kyc` | KYC Protect: profiles, AML screening, monitoring, reports, RBAC |

```go
k := kyc.New(cs)

profile, err := k.GetProfile(ctx, profileID)
if errors.Is(err, creditsafe.ErrNotFound) {
	// ...
}
```

The `kyc` package is generated from Creditsafe's OpenAPI spec. To regenerate
it, for example after Creditsafe updates the spec:

```sh
scripts/generate.sh                         # from kyc/openapi/kycProtect.json
scripts/generate.sh ~/Downloads/kycProtect.json  # replace the spec first
```

## Authentication

Give the client your Creditsafe Connect username and password. It logs in on
the first request, keeps the token, and logs in again shortly before the token
expires (and once more if Creditsafe refuses it early). One `Client` is safe to
share between goroutines.

```go
cs, err := creditsafe.New(
	creditsafe.WithCredentials(os.Getenv("CREDITSAFE_USERNAME"), os.Getenv("CREDITSAFE_PASSWORD")),
	creditsafe.WithSandbox(), // leave out for production
)
```

If you already have a token, use `creditsafe.WithToken(token)` instead. On its
own the client can't renew that token, so once it expires requests fail with
`ErrUnauthorized`.

## Errors

Every non-2xx response comes back as a `*creditsafe.APIError`. Match it by
kind with `errors.Is`:

| Sentinel | Status |
| --- | --- |
| `ErrBadRequest` | 400 |
| `ErrUnauthorized` | 401 |
| `ErrForbidden` | 403 |
| `ErrNotFound` | 404 |
| `ErrConflict` | 409 |
| `ErrRateLimited` | 429 (`APIError.RetryAfter` holds the requested wait) |
| `ErrServer` | 5xx |
| `ErrUnexpected` | any other non-2xx status |

A refused login also matches `ErrAuthentication`. That separates a wrong
username or password from a token that expired or was revoked.

Use `errors.As` to read the details, and quote the correlation ID when you
contact Creditsafe support:

```go
var apiErr *creditsafe.APIError
if errors.As(err, &apiErr) {
	log.Printf("status=%d correlationId=%s message=%s", apiErr.StatusCode, apiErr.CorrelationID, apiErr.Message)
}
```

## License

Apache License 2.0. See [LICENSE](LICENSE).
