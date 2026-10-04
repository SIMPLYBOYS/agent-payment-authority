#!/bin/sh
# Unity steps of the B1-06 shop (docs/demo-ui.md section 2), all in batch
# mode with the pinned editor (ProjectSettings/ProjectVersion.txt):
#   sh scripts/unity/unity.sh scene  OUT   rebuild Assets/Scenes/Shop.unity from limezu-assets.txt
#   sh scripts/unity/unity.sh test   OUT   EditMode and PlayMode tests (NUnit XML in OUT)
#   sh scripts/unity/unity.sh webgl  OUT   uncompressed Web build into Build/WebGL (git-ignored)
# OUT receives the logs. UNITY overrides the editor path.
set -u
step=${1:?scene|test|webgl}
out=${2:?output directory}
root=$(cd "$(dirname "$0")/../.." && pwd)
proj="$root/unity/AgentPaymentAuthority"
version=$(sed -n 's/^m_EditorVersion: //p' "$proj/ProjectSettings/ProjectVersion.txt")
U=${UNITY:-/Applications/Unity/Hub/Editor/$version/Unity.app/Contents/MacOS/Unity}
mkdir -p "$out"
out=$(cd "$out" && pwd) # absolute: Unity resolves relative paths against the project
run() { name=$1; shift; t0=$(date +%s); "$U" -batchmode -nographics -projectPath "$proj" -logFile "$out/unity-$name.log" "$@"; rc=$?; echo "$name exit=$rc seconds=$(($(date +%s) - t0)) editor=$version" | tee -a "$out/unity-exit-codes.txt"; return $rc; }
case $step in
scene) run scene -executeMethod APA.EditorTools.ShopBuilder.Build -quit ;;
test)
	run scene -executeMethod APA.EditorTools.ShopBuilder.Build -quit || exit $?
	run editmode -runTests -testPlatform EditMode -testResults "$out/unity-editmode.xml"; e=$?
	run playmode -runTests -testPlatform PlayMode -testResults "$out/unity-playmode.xml"; p=$?
	[ $e = 0 ] && [ $p = 0 ] ;;
webgl) run webgl -buildTarget WebGL -executeMethod APA.EditorTools.ShopBuilder.BuildWebGL -quit ;;
*) echo "unknown step $step" >&2; exit 2 ;;
esac
