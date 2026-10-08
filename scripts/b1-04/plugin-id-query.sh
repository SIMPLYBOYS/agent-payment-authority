#!/bin/bash
# The operator's wrapper for plugin_id_query.py (docs/cli-poc.md 46.2).
# Usage, from anywhere, as a file (never pasted line by line):
#   bash scripts/b1-04/plugin-id-query.sh <absolute claude path> [evidence parent]
# The evidence parent defaults to artifacts/b1-04 of this repository.
#
# Runs the query at most once and never retries. Its own exit codes:
#   20  usage or python3 missing: the query was not started
#   21  the evidence directory exists or cannot be made: not started, nothing overwritten
#   22  result.txt cannot be created: not started
#   23  the query ran but its evidence is incomplete (it is not run again)
# Otherwise it exits with the query's own code (0, 2-12), kept as soon as
# python3 returns, so nothing after it can change it.
set -u
stop() { echo "STOP（查詢未啟動）：$1" >&2; exit "$2"; }

[ $# -ge 1 ] && [ $# -le 2 ] || stop "usage: plugin-id-query.sh <claude path> [evidence parent]" 20
HERE=$(cd "$(dirname "$0")" && pwd) || stop "no script directory" 20
PY=$(command -v python3) || stop "python3 not found" 20
BASE=${2:-$HERE/../../artifacts/b1-04}
EV="${BASE}/plugin-id-query-$(date -u +%Y%m%dT%H%M%SZ)"
mkdir "$EV" 2>/dev/null || stop "evidence directory exists or cannot be made: ${EV}" 21
( : > "$EV/result.txt" ) 2>/dev/null || stop "result.txt cannot be created: ${EV}" 22

"$PY" -I "$HERE/plugin_id_query.py" --cli "$1" > "$EV/result.txt"
QX=$?

SAVED=1
grep -q '^result=' "$EV/result.txt" 2>/dev/null || SAVED=
( echo "exit=${QX}" > "$EV/exit-code.txt" ) 2>/dev/null || SAVED=
if [ -z "$SAVED" ]; then
  echo "查詢已嘗試，證據不完整（不重跑）：query exit=${QX}；${EV}" >&2
  exit 23
fi
echo "query exit=${QX}；證據：${EV}"
exit "$QX"
