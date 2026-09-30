#!/bin/sh
# Runs INSIDE the verification container started by container-verify.sh.
# Arg: <commit>. Source is /src (read-only); the only writable place is the
# /scratch tmpfs. stdout carries ONLY a tar of the results; logs go to files.
set -u
SHA=$1
O=/scratch/out
mkdir -p /scratch/home /scratch/tmp /scratch/cache "$O"
exec 3>&1 1>&2
finish() {
	rc=$?
	tar -C "$O" -cf - . >&3
	exit "$rc"
}
trap finish EXIT

FAIL=0
probe() {
	name=$1 want=$2
	shift 2
	if timeout -k 5 30 "$@" >"$O/iso-$name.log" 2>&1; then got=allowed; else got=denied; fi
	[ "$got" = "$want" ] && mark=ok || { mark=UNEXPECTED; FAIL=1; }
	echo "$mark  $name: expected $want, got $got"
}
{
	echo "## isolation self-check (inside the container)"
	probe net-external-https denied curl -sS --max-time 8 https://example.com
	probe net-external-ip denied curl -sS --max-time 8 http://1.1.1.1
	probe dns-lookup denied python3 -c 'import socket; socket.getaddrinfo("example.com", 80)'
	# The kernel adds dormant tunnel devices (gre0, tunl0, sit0, ...) to every
	# namespace; the check is that only lo is up and nothing routes elsewhere.
	probe only-loopback-up allowed python3 -c '
import os
up = [n for n in os.listdir("/sys/class/net") if os.path.isdir("/sys/class/net/" + n)
      and int(open("/sys/class/net/" + n + "/flags").read(), 16) & 1]
assert up == ["lo"], up
assert len(open("/proc/net/route").read().splitlines()) == 1, "IPv4 routes present"
assert all(l.split()[-1] == "lo" for l in open("/proc/net/ipv6_route") if l.strip()), "IPv6 route off lo"'
	probe loopback-listen allowed python3 -c 'import socket; s = socket.socket(); s.bind(("127.0.0.1", 0)); s.listen()'
	probe write-source denied touch /src/iso-probe
	probe write-rootfs denied touch /iso-probe
	probe write-toolchain denied touch /usr/local/go/iso-probe
	probe write-scratch allowed touch /scratch/tmp/iso-probe
	probe no-docker-socket allowed sh -c '! test -e /var/run/docker.sock'
	probe no-host-home-mount allowed sh -c '! grep -qE " /(Users|home)" /proc/mounts'
	probe non-root allowed sh -c 'test "$(id -u)" != 0'
	probe no-capabilities allowed grep -Eq '^CapEff:[[:space:]]+0+$' /proc/self/status
	probe no-new-privileges allowed grep -Eq '^NoNewPrivs:[[:space:]]+1$' /proc/self/status
	probe memory-hard-limit denied python3 -c 'b = bytearray(3584 * 1024 * 1024)'
	probe pids-hard-limit denied python3 -c 'import threading, time; [threading.Thread(target=time.sleep, args=(3,)).start() for _ in range(600)]'
	echo
	echo "## identity and environment"
	id
	env | sort
	echo
	echo "## cgroup limits"
	for f in memory.max memory.swap.max pids.max cpu.max; do echo "$f: $(cat /sys/fs/cgroup/$f 2>&1)"; done
	echo
	echo "## ulimit"
	ulimit -a
	echo
	echo "## mounts (source and scratch)"
	grep -E ' /src | /scratch ' /proc/mounts
	df -h /scratch
} >"$O/isolation.txt" 2>&1
if [ "$FAIL" != 0 ]; then
	echo "isolation self-check failed; no checks were run" >>"$O/isolation.txt"
	exit 3
fi

cd /src || exit 2
: >"$O/exit-codes.txt"
FAILED=0
# step NAME SECONDS CMD...: logs to NAME.log, records the exit code, and marks
# the run failed on any non-zero exit (the final exit reports it).
step() {
	name=$1 secs=$2
	shift 2
	timeout -k 10 "$secs" "$@" >"$O/$name.log" 2>&1
	rc=$?
	echo "$name exit=$rc" >>"$O/exit-codes.txt"
	[ "$rc" = 0 ] || FAILED=1
}
if [ "${APA_INJECT_FAIL:-0}" = 1 ]; then
	# Self-test of this script: a deliberate failure next to a real check that
	# passes. The run must end non-zero.
	step injected-failure 30 sh -c 'echo "deliberate failure"; exit 7'
	step check-contracts 900 make check-contracts
else
	step check-contracts 900 make check-contracts
	step check 900 make check
	step race-cover 1800 go test -race -cover ./... -count=1
	step verify-b1-01 900 sh scripts/verify-b1-01.sh
	step guards 600 go test ./internal/contracts/ -run 'TestIntentBindingAcrossEvents|TestDenyEndsTask|TestDeadlineManifestConsistency' -count=1 -v
	step mutation 3600 python3 scripts/verify/mutation-check.py --root /src --out "$O/mutation" "$SHA"
fi
cat "$O/exit-codes.txt"
if [ "$FAILED" != 0 ]; then
	echo "one or more checks failed"
	exit 1
fi
