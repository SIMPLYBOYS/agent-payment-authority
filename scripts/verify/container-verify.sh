#!/bin/sh
# Isolated verification in a Linux container. Docker is a verification tool
# only; the demo does not need it.
# Usage: sh scripts/verify/container-verify.sh [--inject-failure | --inject-export KIND] [commit]
#   (default HEAD). Build the image first with build-image.sh. Needs python3
#   on the host for scripts/verify/evidence_import.py.
#   --inject-failure runs a deliberate failing step next to a passing one.
#   --inject-export KIND (pack-fail | truncate | missing | oversize) breaks the
#   evidence export. Both self-tests must end non-zero.
#
# Evidence export: the container's stdout is an untrusted tar. The host keeps
# at most 64 MiB of it (1 MiB of stderr) and kills the run beyond that, then
# evidence_import.py validates it in a staging directory (regular files on an
# allowlist, no links or special files, no traversal, size limits, manifest
# hashes, required files) before copying it to <run>/container/. A rejected
# archive is kept unextracted as <run>/rejected-archive.tar. Any failure in
# packing, transfer, validation or saving makes the script exit non-zero.
#
# Test-phase isolation (every flag is recorded in docker-run.txt):
# - --network none: own network namespace with only lo, no external network.
# - --read-only root filesystem; the commit is a clone bind-mounted read-only
#   at /src; the only writable place is a 3 GB tmpfs at /scratch.
# - Hard limits: --memory 3g (no extra swap), --pids-limit 512, --cpus 2,
#   ulimits for open files, CPU seconds and file size; wall time 90 minutes.
# - --user 10001, --cap-drop ALL, no-new-privileges; not privileged, not host
#   network; no host home, credentials or Docker socket are mounted; only the
#   variables listed below are set.
# Results go to a new artifacts/verify-container/runs/<commit12>-<UTC time>/.
set -eu

ROOT=$(cd "$(dirname "$0")/../.." && pwd)
IMAGE=${APA_VERIFY_IMAGE:-apa-verify:b1-02}
INJECT=0 EXPORT_FAULT="" MODE=full
case "${1:-}" in
--inject-failure)
	INJECT=1 MODE=inject
	shift
	;;
--inject-export)
	case "${2:-}" in
	pack-fail | truncate | missing | oversize) EXPORT_FAULT=$2 MODE=export-fault ;;
	*)
		echo "--inject-export needs pack-fail, truncate, missing or oversize" >&2
		exit 64
		;;
	esac
	shift 2
	;;
esac
SHA=$(git -C "$ROOT" rev-parse "${1:-HEAD}^{commit}")
RUN="$ROOT/artifacts/verify-container/runs/$(printf '%.12s' "$SHA")-$(date -u +%Y%m%dT%H%M%SZ)"
mkdir -p "$(dirname "$RUN")"
mkdir "$RUN"

BASE=$(mktemp -d "${TMPDIR:-/tmp}/apa-ctr.XXXXXX")
NAME="apa-verify-$$"
cleanup() {
	docker rm -f "$NAME" >/dev/null 2>&1 || true
	chmod -R u+w "$BASE" 2>/dev/null || true
	rm -rf "$BASE"
}
trap cleanup EXIT INT TERM

git clone --quiet --no-hardlinks "$ROOT" "$BASE/src"
git -C "$BASE/src" checkout --quiet --detach "$SHA"
chmod -R a-w "$BASE/src"
IMAGE_ID=$(docker image inspect --format '{{.Id}}' "$IMAGE")

set -- docker run --name "$NAME" --rm \
	--network none --read-only \
	--tmpfs /scratch:rw,exec,nosuid,nodev,size=3g,mode=1777 \
	--mount "type=bind,source=$BASE/src,target=/src,readonly" \
	--user 10001:10001 --cap-drop ALL --security-opt no-new-privileges \
	--memory 3g --memory-swap 3g --pids-limit 512 --cpus 2 \
	--ulimit nofile=4096:4096 --ulimit cpu=3600:3600 --ulimit fsize=1073741824:1073741824 \
	--env HOME=/scratch/home --env TMPDIR=/scratch/tmp \
	--env GOCACHE=/scratch/cache/go-build --env GOMODCACHE=/scratch/cache/mod --env GOPATH=/scratch/cache/gopath \
	--env GOPROXY=off --env GOSUMDB=off --env GOTOOLCHAIN=local --env GOFLAGS=-mod=readonly \
	--env GIT_CONFIG_NOSYSTEM=1 --env GIT_CONFIG_COUNT=1 --env GIT_CONFIG_KEY_0=safe.directory --env GIT_CONFIG_VALUE_0=/src \
	--env PYTHONDONTWRITEBYTECODE=1 --env APA_INJECT_FAIL="$INJECT" --env APA_INJECT_EXPORT="$EXPORT_FAULT" \
	--workdir /src "$IMAGE_ID" sh /src/scripts/verify/container-inner.sh "$SHA"
printf '%s\n' "$*" >"$RUN/docker-run.txt"

# Transfer: capped capture with a 90-minute wall-time limit (cleanup removes
# the container if the client is killed).
EVIDENCE="$ROOT/scripts/verify/evidence_import.py"
crc=0
python3 "$EVIDENCE" capture --out "$BASE/out.tar" --err "$RUN/container-stderr.log" \
	--max-out 67108864 --max-err 1048576 --timeout 5400 -- "$@" || crc=$?

# Validate in staging, then save to <run>/container/; never extract directly.
irc=0
python3 "$EVIDENCE" import --archive "$BASE/out.tar" --staging "$BASE/staging" \
	--dest "$RUN/container" --mode "$MODE" >"$RUN/import.txt" 2>&1 || irc=$?
if [ "$irc" != 0 ] && [ -f "$BASE/out.tar" ]; then
	cp "$BASE/out.tar" "$RUN/rejected-archive.tar" # kept unextracted for inspection
fi

# Any failure is non-zero: the container/capture code first, else the import.
rc=$crc
[ "$rc" != 0 ] || rc=$irc
{
	echo "commit: $SHA"
	echo "image: $IMAGE ($IMAGE_ID)"
	case "$MODE" in
	inject) echo "mode: self-test with an injected failing step" ;;
	export-fault) echo "mode: self-test with export fault '$EXPORT_FAULT'" ;;
	*) echo "mode: full verification" ;;
	esac
	echo "container/capture exit: $crc"
	echo "evidence import exit: $irc ($(head -1 "$RUN/import.txt"))"
	echo "overall exit: $rc"
} >"$RUN/run.txt"
cat "$RUN/run.txt"
echo "run directory: $RUN"
exit "$rc"
