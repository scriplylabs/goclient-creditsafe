#!/usr/bin/env bash
# Regenerates kyc/kyc.gen.go from Creditsafe's KYC Protect OpenAPI spec.
#
#   scripts/generate.sh                      # regenerate from kyc/openapi/kycProtect.json
#   scripts/generate.sh ~/Downloads/kyc.json # replace the spec with a new download, then regenerate
#
# Inputs, all under kyc/:
#   openapi/kycProtect.json   the spec, as Creditsafe publishes it (don't edit it)
#   openapi/overlay.yaml      Go method names for each operation
#   oapi-codegen.yaml         generator config
#   templates/*.tmpl          template overrides (see each file's header)
#
# When Creditsafe adds an operation, give it a name in openapi/overlay.yaml;
# TestOverlayNamesEveryOperation fails until you do.
set -euo pipefail

# Bump deliberately: templates/client.tmpl is a modified copy of this version's.
OAPI_CODEGEN_VERSION=v2.8.2

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
kyc="$root/kyc"

if [[ $# -gt 1 ]]; then
	echo "usage: $0 [path/to/new/kycProtect.json]" >&2
	exit 2
fi

if [[ $# -eq 1 ]]; then
	cp "$1" "$kyc/openapi/kycProtect.json"
	echo "copied $1 to kyc/openapi/kycProtect.json"
fi

if [[ ! -f "$kyc/openapi/kycProtect.json" ]]; then
	echo "missing kyc/openapi/kycProtect.json; pass the downloaded spec as an argument" >&2
	exit 1
fi

echo "generating kyc/kyc.gen.go with oapi-codegen $OAPI_CODEGEN_VERSION"
(
	cd "$kyc"
	go run "github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@$OAPI_CODEGEN_VERSION" \
		-config oapi-codegen.yaml openapi/kycProtect.json
)

cd "$root"
go mod tidy

unformatted="$(gofmt -l kyc)"
if [[ -n "$unformatted" ]]; then
	echo "gofmt reports unformatted files: $unformatted" >&2
	exit 1
fi

go build ./...
go vet ./...
echo "ok: kyc/kyc.gen.go regenerated; run go test ./... before committing"
