#!/bin/sh
# Preparation phase: builds the verification image. Network is allowed here
# (pulling the pinned base images) and only here; the test phase runs with
# --network none. Usage: sh scripts/verify/build-image.sh
# Records the image ID and tool versions in a new
# artifacts/verify-container/image/<UTC time>/ directory.
set -eu

ROOT=$(cd "$(dirname "$0")/../.." && pwd)
TAG=${APA_VERIFY_IMAGE:-apa-verify:b1-02}
OUT="$ROOT/artifacts/verify-container/image/$(date -u +%Y%m%dT%H%M%SZ)"
mkdir -p "$(dirname "$OUT")"
mkdir "$OUT"

docker build --quiet -t "$TAG" "$ROOT/scripts/verify" >"$OUT/build.log" 2>&1
{
	echo "tag: $TAG"
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
