#!/bin/sh
# Preparation phase: builds the verification image. Network is allowed here
# and only here: pulling the pinned base images and downloading the Go modules
# listed in the commit's go.mod/go.sum (proxy.golang.org, checked against
# sum.golang.org). The test phase runs with --network none.
# Usage: sh scripts/verify/build-image.sh [commit]   (default HEAD)
# The build context is a temporary directory holding only the Dockerfile and
# the commit's go.mod and go.sum, taken from git, not the working tree.
# Records the image ID, dependency hashes and tool versions in a new
# artifacts/verify-container/image/<UTC time>/ directory.
set -eu

ROOT=$(cd "$(dirname "$0")/../.." && pwd)
TAG=${APA_VERIFY_IMAGE:-apa-verify:b1-05}
SHA=$(git -C "$ROOT" rev-parse "${1:-HEAD}^{commit}")
OUT="$ROOT/artifacts/verify-container/image/$(date -u +%Y%m%dT%H%M%SZ)"
mkdir -p "$(dirname "$OUT")"
mkdir "$OUT"
CTX=$(mktemp -d "${TMPDIR:-/tmp}/apa-image.XXXXXX")
trap 'rm -rf "$CTX"' EXIT INT TERM
cp "$ROOT/scripts/verify/Dockerfile" "$CTX/"
git -C "$ROOT" show "$SHA:go.mod" >"$CTX/go.mod"
git -C "$ROOT" show "$SHA:go.sum" >"$CTX/go.sum"
ls -A "$CTX" >"$OUT/build-context.txt"

docker build --quiet -t "$TAG" "$CTX" >"$OUT/build.log" 2>&1
{
	echo "tag: $TAG"
	echo "dependencies from commit: $SHA"
	echo "build context: $(tr '\n' ' ' <"$OUT/build-context.txt")"
	echo "go.mod/go.sum recorded in the image:"
	docker run --rm --network none "$TAG" cat /opt/apa/deps.sha256 | sed 's/^/  /'
	docker image inspect --format 'image id: {{.Id}}' "$TAG"
	echo "base images (pinned in scripts/verify/Dockerfile):"
	grep '^FROM' "$ROOT/scripts/verify/Dockerfile" | sed 's/^/  /'
	for base in $(grep '^FROM' "$ROOT/scripts/verify/Dockerfile" | awk '{print $2}'); do
		docker image inspect --format "  $base -> {{.Id}}" "$base"
	done
	echo "tool versions (read with --network none):"
	docker run --rm --network none "$TAG" sh -c '
		go version; echo "node $(node --version)"; python3 --version; git --version
		gcc --version | head -1; make --version | head -1; curl --version | head -1
		dpkg-query -W -f "\${Package} \${Version}\n" python3 procps git curl make gcc' | sed 's/^/  /'
} >"$OUT/image.txt"
cat "$OUT/image.txt"
