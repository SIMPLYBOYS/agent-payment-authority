#!/bin/sh
# V1 on /opt/apa-signer (option B, docs/commercial-v1.md 15): stage the voice
# sidecar's files under var/v1-opt/stage/voice for Aaron to install as root.
# Nothing here uses sudo or touches /opt.
#
#   sh scripts/demo/v1-opt-stage.sh
#
# The sidecar runs as apa-signer, so nothing it loads may be writable by
# another account (P1-b): not Homebrew's Python (owned by mac), not a
# library found through /usr/local/lib (mac) or /tmp (anyone). So:
# - Python is python-build-standalone 3.12.6 (uv python install, downloaded
#   once with Aaron's approval into var/voice/standalone), self-contained
#   and relocatable;
# - the pinned packages (var/voice/requirements.txt) are installed into a
#   copy of it offline, from var/voice/.uv-cache;
# - every absolute LC_RPATH left by the wheels' build machines is deleted
#   (only @loader_path / @executable_path ones stay) and the files are
#   ad-hoc signed again;
# - models/ and sidecar.py are copied; manifest.sha256 lists every file.
set -eu
root=$(cd "$(dirname "$0")/../.." && pwd)
src="$root/var/voice/standalone/cpython-3.12.6-macos-x86_64-none"
out="$root/var/v1-opt/stage"
[ -x "$src/bin/python3.12" ] || { echo "v1-opt-stage: no standalone Python in $src (uv python install 3.12.6)" >&2; exit 1; }
[ ! -e "$out" ] || { echo "v1-opt-stage: $out exists; remove it yourself to stage again" >&2; exit 1; }
mkdir -p "$out/voice"
chmod 700 "$root/var/v1-opt"
cp -R "$src" "$out/voice/python"
UV_CACHE_DIR="$root/var/voice/.uv-cache" uv pip install --offline --python "$out/voice/python/bin/python3.12" --break-system-packages -q -r "$root/var/voice/requirements.txt"
# The wheels' scripts are not used (the sidecar runs python3.12 -I) and their
# shebangs name the staging path.
# -L: the speech model's label_encoder.ckpt is a symlink to an absolute path
# under this repo, which apa-signer cannot read (found at P2-b's /opt step):
# its file is copied instead.
cp -RL "$root/var/voice/models" "$out/voice/models"
cp "$root/voice/sidecar.py" "$out/voice/sidecar.py"
# No symlink may point outside the stage (apa-signer reads none of mac's files).
if find "$out/voice" -type l -lname '/*' | grep -q .; then
  find "$out/voice" -type l -lname '/*' >&2
  echo "v1-opt-stage: absolute symlinks in the stage (above); nothing to install" >&2
  exit 1
fi

n=0
find "$out/voice/python" -type f \( -name '*.so' -o -name '*.dylib' -o -perm -u+x \) | while read -r f; do
  file -b "$f" | grep -q '^Mach-O' || continue
  changed=0
  for r in $(otool -l "$f" | awk '/cmd LC_RPATH/{getline; getline; print $2}' | grep -v '^@' || true); do
    install_name_tool -delete_rpath "$r" "$f" 2>/dev/null && changed=1
  done
  if [ $changed -eq 1 ]; then
    codesign -f -s - "$f" 2>/dev/null
    echo "v1-opt-stage: absolute rpaths removed: ${f#$out/voice/}" >&2
  fi
done
left=$(find "$out/voice/python" -type f \( -name '*.so' -o -name '*.dylib' \) -exec otool -l {} \; | awk '/cmd LC_RPATH/{getline; getline; print $2}' | grep -vc '^@' || true)
[ "$left" -eq 0 ] || { echo "v1-opt-stage: $left absolute rpaths left" >&2; exit 1; }

(cd "$out" && find voice -type f | LC_ALL=C sort | xargs -I{} shasum -a 256 {} > manifest.sha256)
echo "v1-opt-stage: staged $out ($(wc -l < "$out/manifest.sha256" | tr -d ' ') files, $(du -sh "$out/voice" | cut -f1)); manifest $out/manifest.sha256"
