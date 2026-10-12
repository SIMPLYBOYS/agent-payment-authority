#!/bin/sh
# Runs one m1-check plan in an isolated Linux container (P1-b review rounds).
# Usage: sh scripts/verify/container-plan.sh PLAN OUTDIR [commit]  (default HEAD)
#
# The isolation flags are container-verify.sh's: --network none (only lo),
# --read-only root filesystem (the image's Go toolchain and tools are not
# writable), the commit's clone bind-mounted read-only at /src, a tmpfs
# /scratch for HOME, TMPDIR and the build cache, --user 10001, --cap-drop ALL,
# no-new-privileges, memory/pids/cpu and ulimit caps, a wall-time limit; no
# host home, credentials or Docker socket; only the variables below are set.
# Go modules: a cache built on the host ONLY from the local module cache
# (GOPROXY=file://..., no network), checked against the commit's go.sum,
# mounted read-only. OUTDIR (new, on the host) is the one writable mount, for
# the m1-check evidence. Image: the pinned verification image (Dockerfile).
set -eu

ROOT=$(cd "$(dirname "$0")/../.." && pwd)
IMAGE=${APA_VERIFY_IMAGE:-apa-verify:b1-05}
PLAN=$1 OUT=$2
SHA=$(git -C "$ROOT" rev-parse "${3:-HEAD}^{commit}")
mkdir "$OUT"
OUT=$(cd "$OUT" && pwd)
chmod 0777 "$OUT"

BASE=$(mktemp -d "${APA_PLAN_TMP:-${TMPDIR:-/tmp}}/apa-plan.XXXXXX")
NAME="apa-plan-$$"
cleanup() {
	docker rm -f "$NAME" >/dev/null 2>&1 || true
	chmod -R u+w "$BASE" 2>/dev/null || true
	rm -rf "$BASE"
}
trap cleanup EXIT INT TERM

git clone --quiet --no-hardlinks "$ROOT" "$BASE/src"
git -C "$BASE/src" checkout --quiet --detach "$SHA"
HOSTMOD=$(go env GOMODCACHE) # read before GOMODCACHE is set below
(cd "$BASE/src" && GOMODCACHE="$BASE/gomodcache" GOPROXY="file://$HOSTMOD/cache/download" GOSUMDB=off \
	GOTOOLCHAIN=local GOFLAGS=-mod=readonly go mod download) >"$OUT/modules.txt" 2>&1
echo "modules: from the local module cache only (GOPROXY=file://...), checked against go.sum of $SHA" >>"$OUT/modules.txt"
chmod -R a+rX,a-w "$BASE/src" "$BASE/gomodcache"

set -- docker run --name "$NAME" --rm \
	--network none --read-only \
	--tmpfs /scratch:rw,exec,nosuid,nodev,size=3g,mode=1777 \
	--mount "type=bind,source=$BASE/src,target=/src,readonly" \
	--mount "type=bind,source=$BASE/gomodcache,target=/opt/p1b/gomodcache,readonly" \
	--mount "type=bind,source=$OUT,target=/out" \
	--user 10001:10001 --cap-drop ALL --security-opt no-new-privileges \
	--memory 3g --memory-swap 3g --pids-limit 512 --cpus 4 \
	--ulimit nofile=4096:4096 --ulimit cpu=3600:3600 --ulimit fsize=1073741824:1073741824 \
	--env HOME=/scratch/home --env TMPDIR=/scratch/tmp \
	--env GOCACHE=/scratch/cache/go-build --env GOMODCACHE=/opt/p1b/gomodcache --env GOPATH=/scratch/cache/gopath \
	--env GOPROXY=off --env GOSUMDB=off --env GOTOOLCHAIN=local --env GOFLAGS=-mod=readonly \
	--env GIT_CONFIG_NOSYSTEM=1 --env GIT_CONFIG_COUNT=1 --env GIT_CONFIG_KEY_0=safe.directory --env GIT_CONFIG_VALUE_0=/src \
	--env PYTHONDONTWRITEBYTECODE=1 \
	--workdir /src "$(docker image inspect --format '{{.Id}}' "$IMAGE")" sh -c '
mkdir -p /scratch/home /scratch/tmp /scratch/cache
probe() { if timeout -k 5 30 sh -c "$2" >/dev/null 2>&1; then got=allowed; else got=denied; fi; echo "$1: $got"; }
{
	probe "external https" "curl -sS --max-time 8 https://example.com"
	probe "external ip" "curl -sS --max-time 8 http://1.1.1.1"
	probe "write /src" "touch /src/probe"
	probe "write the Go toolchain" "touch \$(go env GOROOT)/probe"
	probe "write the module cache" "touch /opt/p1b/gomodcache/probe"
	echo "network interfaces up: $(for n in /sys/class/net/*; do [ $(( $(cat $n/flags) & 1 )) = 1 ] && basename $n; done | tr "\n" " ")"
	echo "uid: $(id -u)"; go version; python3 --version; git --version
	echo "environment:"; env | sort | sed "s/^/  /"
} >/out/isolation.txt 2>&1
exec timeout -k 10 2700 go run ./cmd/m1-check --plan "$0" --out /out
' "$PLAN"
printf '%s\n' "$*" >"$OUT/docker-run.txt"
echo "commit: $SHA" >>"$OUT/docker-run.txt"
echo "image: $IMAGE" >>"$OUT/docker-run.txt"
rc=0
"$@" >"$OUT/summary.json" 2>"$OUT/progress.txt" || rc=$?
echo "exit: $rc" >>"$OUT/docker-run.txt"
exit "$rc"
