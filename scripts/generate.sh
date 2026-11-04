#!/usr/bin/env bash
# Regenerates every generated package (kyc, creditrisk) from Creditsafe's
# OpenAPI specs.
#
#   scripts/generate.sh                                      # regenerate all
#   scripts/generate.sh creditrisk ~/Downloads/creditAndRisk.json  # replace a spec first
#
# A generated package is a directory holding oapi-codegen.yaml. Its inputs:
#   <pkg>/openapi/<name>.json   the spec, as Creditsafe publishes it (exactly
#                               one .json file; don't edit it)
#   <pkg>/openapi/overlay.yaml  Go method names for each operation
#   <pkg>/oapi-codegen.yaml     generator config
#   codegen/templates/*.tmpl    template overrides shared by all packages
#
# When Creditsafe adds an operation, give it a name in the package's
# overlay; its TestOverlayNamesEveryOperation fails until you do.
set -euo pipefail

# Bump deliberately: codegen/templates/client.tmpl is a modified copy of this version's.
OAPI_CODEGEN_VERSION=v2.8.2

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

# spec_of prints the one spec file of package $1, or fails.
spec_of() {
	local specs=("$1"/openapi/*.json)
	if [[ ${#specs[@]} -ne 1 || ! -f "${specs[0]}" ]]; then
		echo "$1/openapi must hold exactly one .json spec" >&2
		return 1
	fi
	echo "${specs[0]}"
}

case $# in
0) ;;
2)
	pkg="${1%/}"
	if [[ ! -f "$pkg/oapi-codegen.yaml" ]]; then
		echo "$pkg isn't a generated package (no $pkg/oapi-codegen.yaml)" >&2
		exit 1
	fi
	spec="$(spec_of "$pkg")"
	cp "$2" "$spec"
	echo "copied $2 to $spec"
	;;
*)
	echo "usage: $0 [package path/to/new-spec.json]" >&2
	exit 2
	;;
esac

packages=()
for config in */oapi-codegen.yaml; do
	packages+=("$(dirname "$config")")
done

for pkg in "${packages[@]}"; do
	spec="$(spec_of "$pkg")"
	echo "generating $pkg from $spec with oapi-codegen $OAPI_CODEGEN_VERSION"
	(
		cd "$pkg"
		go run "github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@$OAPI_CODEGEN_VERSION" \
			-config oapi-codegen.yaml "${spec#"$pkg"/}"
	)
done

go mod tidy

unformatted="$(gofmt -l "${packages[@]}")"
if [[ -n "$unformatted" ]]; then
	echo "gofmt reports unformatted files: $unformatted" >&2
	exit 1
fi

go build ./...
go vet ./...
echo "ok: regenerated ${packages[*]}; run go test ./... before committing"
