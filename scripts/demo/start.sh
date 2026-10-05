#!/bin/sh
# make demo: the fake demo in one command (B1-06 slice 3.1,
# docs/demo-operation-design.md section 3). It checks the Unity Web build and
# the LimeZu import (missing ones are reported, the demo still starts and the
# page says the scene is not ready), builds the two Go binaries (Go's build
# cache; seconds) and runs the demo host with the fixed fake plan, exactly as
# make demo-integration: task-a and task-b run once at start. It never builds
# Unity, runs tests, starts Docker, runs the real Claude Code CLI or prints a
# token. A refused start (address in use, database locked, not the fake CLI,
# Store recovery) ends with the host's own message and its exit code.
#
# Environment: APA_ADDR (default 127.0.0.1:8402), APA_DB_PATH,
# APA_DEMO_TOKEN_FILE (as the host reads them); APA_UNITY_PROJECT (default
# unity/AgentPaymentAuthority); APA_DEMO_BIN, a directory with demo and
# fake-claude already built (tests), skips the build.
set -u
root=$(cd "$(dirname "$0")/../.." && pwd)
cd "$root" || exit 1
proj=${APA_UNITY_PROJECT:-unity/AgentPaymentAuthority}
build=$proj/Build/WebGL/Build
missing=""
for f in WebGL.loader.js WebGL.framework.js WebGL.wasm WebGL.data; do
	[ -f "${build}/${f}" ] || missing="${missing} ${f}"
done
if [ -n "$missing" ]; then
	echo "demo: Unity 建置不完整（缺:${missing}）。商店會顯示「Unity 場景未就緒」；首次準備見 docs/demo-ui.md 第 5 節" >&2
fi
if [ ! -f "$proj/Assets/LimeZu/IMPORTED.txt" ]; then
	echo "demo: 找不到 LimeZu 匯入紀錄（${proj}/Assets/LimeZu/IMPORTED.txt）。若有建置，可能是 placeholder；實際以頁面上 Unity 的回報為準" >&2
fi
if [ -n "${APA_DEMO_BIN:-}" ]; then
	bin=$APA_DEMO_BIN
else
	bin=bin
	go build -o bin/demo ./cmd/demo || exit $?
	go build -o bin/fake-claude ./cmd/fake-claude || exit $?
fi
addr=${APA_ADDR:-127.0.0.1:8402}
echo "demo: 啟動中。主持人頁 http://${addr}/host（一般 viewer 頁 http://${addr}/）；token 檔 ${APA_DEMO_TOKEN_FILE:-var/demo-tokens.json}，本腳本不印 token" >&2
APA_ADDR=$addr exec "$bin/demo" --fake-cli "$bin/fake-claude" --plan testdata/demo/fake-plan.json --unity-build "$build"
