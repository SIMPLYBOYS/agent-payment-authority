#!/bin/sh
# B1-01 acceptance run: sh scripts/verify-b1-01.sh
#
# Every check that builds, writes a .env or starts a server runs in a throwaway
# copy of the non-ignored files, on ports the OS picks. The repo this script
# lives in is only read. Only PIDs started here are signalled.
# Exit 0 only if every check passed; 1 if any failed; 2 if it could not set up.
set -u

ROOT=$(cd "$(dirname "$0")/.." && pwd) || exit 2
TMP=$(mktemp -d "${TMPDIR:-/tmp}/apa-verify.XXXXXX") || exit 2
WORK=$TMP/repo
OUT=$TMP/out
FAILS=0
BPID= # bounded command in flight, if any
MK=   # background `make dev` in flight, if any
HEALTH='{"status":"ok","mode":"fixture","live_cli":false,"claude_login":"not_checked","payment_capability":"unavailable"}'

# The caller's settings must not leak into any case; each one passes its own.
unset APA_ADDR APA_LIVE_CLI

say() { printf '\n### %s\n' "$*"; }
pass() { printf 'PASS  %s\n' "$*"; }
fail() {
	printf 'FAIL  %s\n' "$*"
	FAILS=$((FAILS + 1))
}
show() { sed 's/^/      | /' "$1"; }

kill_tree() {
	for c in $(pgrep -P "$1"); do kill_tree "$c" "${2:--TERM}"; done
	kill "${2:--TERM}" "$1" 2>/dev/null
}

cleanup() {
	for p in $BPID $MK; do kill_tree "$p" -KILL; done
	rm -rf "$TMP"
}
trap cleanup EXIT
trap 'exit 130' INT TERM

# What this run must leave exactly as it found it.
repo_state() {
	for f in .env bin/.probe; do
		if [ -e "$ROOT/$f" ]; then printf '%s %s\n' "$f" "$(cksum <"$ROOT/$f")"; else printf '%s absent\n' "$f"; fi
	done
	git -C "$ROOT" status --porcelain --ignored
}

# bounded SECONDS CMD...: output goes to $OUT, exit code to RC. A command still
# running after SECONDS is killed with its children and RC is "timeout", so an
# expected refusal that starts a server instead cannot hang or pass the run.
bounded() {
	limit=$1
	shift
	rm -f "$OUT.rc"
	("$@" >"$OUT" 2>&1; echo $? >"$OUT.rc") &
	BPID=$!
	i=0
	while [ ! -s "$OUT.rc" ] && [ "$i" -lt $((limit * 10)) ]; do
		sleep 0.1
		i=$((i + 1))
	done
	if [ -s "$OUT.rc" ]; then
		RC=$(cat "$OUT.rc")
	else
		kill_tree "$BPID" -KILL
		RC=timeout
	fi
	wait "$BPID" 2>/dev/null
	BPID=
}

# expect_ok NAME SECONDS CMD...: passes only on exit 0.
expect_ok() {
	name=$1
	shift
	bounded "$@"
	if [ "$RC" = 0 ]; then pass "$name"; else
		fail "$name (exit $RC, wanted 0)"
		show "$OUT"
	fi
}

# expect_refused NAME PATTERN CMD...: passes only if CMD exits non-zero by
# itself, says PATTERN, and never reported a listener.
expect_refused() {
	name=$1
	pat=$2
	shift 2
	bounded 20 "$@"
	if [ "$RC" != 0 ] && [ "$RC" != timeout ] && grep -q -- "$pat" "$OUT" && ! grep -q 'listening on' "$OUT"; then
		pass "$name (exit $RC)"
	else
		fail "$name (exit $RC, wanted a non-zero exit saying \"$pat\" and no listener)"
		show "$OUT"
	fi
}

# expect_env_refused NAME PATTERN: same, for `make dev` with .env written from stdin.
expect_env_refused() {
	cat >.env
	expect_refused "$@" make dev
	rm -f .env
}

# start_dev LOG [VAR=VALUE...]: runs `make dev` in the background and sets MK
# (make), DEMO (the server it became) and ADDR (what the server says it bound).
start_dev() {
	log=$1
	shift
	ADDR=
	DEMO=
	env "$@" make dev >"$log" 2>&1 &
	MK=$!
	i=0
	while [ "$i" -lt 300 ]; do
		ADDR=$(sed -n 's|.*listening on http://\([^ ]*\).*|\1|p' "$log" | head -1)
		[ -n "$ADDR" ] && break
		kill -0 "$MK" 2>/dev/null || break
		sleep 0.1
		i=$((i + 1))
	done
	[ -n "$ADDR" ] && DEMO=$(pgrep -P "$MK" | head -1) && [ -n "$DEMO" ] && return 0
	kill_tree "$MK" -KILL
	wait "$MK" 2>/dev/null
	MK=
	return 1
}

check_health() {
	got=$(curl -sS --noproxy '*' --max-time 5 -o "$OUT" -w '%{http_code} %{content_type}' "http://$ADDR/healthz" 2>&1)
	if [ "$got" = "200 application/json" ] && [ "$(cat "$OUT")" = "$HEALTH" ]; then
		pass "$1: GET /healthz on $ADDR is 200 with the expected body"
	else
		fail "$1: GET /healthz on $ADDR gave \"$got\""
		show "$OUT"
	fi
}

# stop_dev NAME SIGNAL: signals the server only, then requires a clean exit,
# no process left, and nothing answering on the address.
stop_dev() {
	kill "-$2" "$DEMO"
	i=0
	while kill -0 "$DEMO" 2>/dev/null && [ "$i" -lt 100 ]; do
		sleep 0.1
		i=$((i + 1))
	done
	if kill -0 "$DEMO" 2>/dev/null; then
		fail "$1: server $DEMO still running 10s after SIG$2"
		kill_tree "$MK" -KILL
		wait "$MK" 2>/dev/null
		MK=
		return
	fi
	wait "$MK"
	rc=$?
	MK=
	if [ "$rc" = 0 ]; then pass "$1: SIG$2 stopped the server, make exit 0"; else fail "$1: make exit $rc after SIG$2, wanted 0"; fi
	if curl -sS --noproxy '*' --max-time 2 -o /dev/null "http://$ADDR/healthz" 2>/dev/null; then
		fail "$1: $ADDR still answers after the stop"
	else
		pass "$1: $ADDR no longer answers"
	fi
}

say "environment"
go version
date -u +%Y-%m-%dT%H:%M:%SZ
echo "repo: $ROOT"

BEFORE=$(repo_state) || {
	echo "ABORT: $ROOT is not a git work tree"
	exit 2
}
mkdir "$WORK" || exit 2
(cd "$ROOT" && git ls-files -co --exclude-standard -z | tar -cf - --null -T -) | tar -xf - -C "$WORK"
[ -f "$WORK/Makefile" ] && [ -f "$WORK/go.mod" ] || {
	echo "ABORT: could not copy the non-ignored files to $WORK"
	exit 2
}
cd "$WORK" || exit 2
echo "work copy: $WORK (non-ignored files only)"

say "make check"
expect_ok "go vet, go test and go build" 300 make check
expect_ok "make build" 300 make build

say "refused by the server"
expect_refused "make dev without .env or APA_ADDR" "APA_ADDR is required" make dev
expect_refused "APA_LIVE_CLI=true" "APA_LIVE_CLI=true is not available" env APA_ADDR=127.0.0.1:0 APA_LIVE_CLI=true ./bin/demo
expect_refused "non-loopback address" "must be a loopback IP" env APA_ADDR=0.0.0.0:0 ./bin/demo
expect_env_refused ".env with APA_LIVE_CLI=true" "APA_LIVE_CLI=true is not available" <<'EOF'
APA_ADDR=127.0.0.1:0
APA_LIVE_CLI=true
EOF
expect_env_refused ".env with a non-loopback address" "must be a loopback IP" <<'EOF'
APA_ADDR=0.0.0.0:0
EOF

say "refused while loading .env"
expect_env_refused "space before =" ".env line(s) 2 " <<'EOF'
APA_ADDR=127.0.0.1:0
APA_LIVE_CLI =true
EOF
expect_env_refused "bad line followed by valid ones" ".env line(s) 1 " <<'EOF'
APA_LIVE_CLI =true
APA_ADDR=127.0.0.1:0
APA_LIVE_CLI=false
EOF
expect_env_refused "space after = (would run a command)" ".env line(s) 2 " <<'EOF'
APA_ADDR=127.0.0.1:0
APA_LIVE_CLI= true
EOF
expect_env_refused "quoted value" ".env line(s) 1 " <<'EOF'
APA_ADDR="127.0.0.1:0"
EOF
expect_env_refused "shell syntax in a value" ".env line(s) 2 " <<'EOF'
APA_ADDR=127.0.0.1:0
APA_X=$(touch env-was-executed)
EOF
if [ -e env-was-executed ]; then fail ".env content was executed"; else pass ".env content was not executed"; fi

say "start from .env, port conflict, stop with SIGINT"
cat >.env <<'EOF'
# comments and blank lines are fine

APA_ADDR=127.0.0.1:0
APA_LIVE_CLI=false
EOF
if start_dev "$TMP/round1.log"; then
	echo "make pid=$MK server pid=$DEMO addr=$ADDR"
	check_health "round 1"
	FIRST=$ADDR
	expect_refused "second server on $FIRST" "already in use" env "APA_ADDR=$FIRST" ./bin/demo
	check_health "round 1 after the conflict"
	stop_dev "round 1" INT
else
	fail "round 1: make dev with a valid .env never listened"
	show "$TMP/round1.log"
	FIRST=
fi
rm -f .env

say "start from the environment on the same port, stop with SIGTERM"
if [ -z "$FIRST" ]; then
	fail "round 2: skipped, round 1 gave no address to reuse"
elif start_dev "$TMP/round2.log" "APA_ADDR=$FIRST"; then
	echo "make pid=$MK server pid=$DEMO addr=$ADDR"
	if [ "$ADDR" = "$FIRST" ]; then pass "round 2: bound $FIRST again, so round 1 released it"; else fail "round 2: bound $ADDR, wanted $FIRST"; fi
	check_health "round 2"
	stop_dev "round 2" TERM
else
	fail "round 2: make dev without .env never listened on $FIRST"
	show "$TMP/round2.log"
fi

say "ignore rules (read-only, no probe files)"
for p in .env .env.local bin/demo var/x artifacts/m1/x key.pem keys/x a.wav assets/limezu/x.png; do
	if git -C "$ROOT" check-ignore -q "$p"; then pass "ignored: $p"; else fail "not ignored: $p"; fi
done
for p in .env.example testdata/x.json scripts/verify-b1-01.sh; do
	if git -C "$ROOT" check-ignore -q "$p"; then fail "ignored but must be tracked: $p"; else pass "tracked: $p"; fi
done

say "repo left as found"
if [ "$BEFORE" = "$(repo_state)" ]; then
	pass ".env, bin/.probe and git status are unchanged"
else
	fail "the repo changed during the run"
	echo "$BEFORE" >"$OUT"
	show "$OUT"
	repo_state | show /dev/stdin
fi

say "result"
if [ "$FAILS" -gt 0 ]; then
	echo "FAILED: $FAILS check(s)"
	exit 1
fi
echo "all checks passed"
