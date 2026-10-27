#!/bin/sh
# Copies exactly the LimeZu files listed in unity/AgentPaymentAuthority/
# limezu-assets.txt from LIMEZU_DIR (the folder holding
# Modern_Interiors_v41.4/ and, for the town, Modern_Exteriors_v42.3/) into
# the Unity project's Assets/LimeZu/, which git ignores, and records each
# copy's sha256 in Assets/LimeZu/IMPORTED.txt. A file already there with
# the same bytes is left as it is. Nothing else is read or copied. Usage:
#   LIMEZU_DIR=/path/to/limezu sh scripts/unity/import-limezu.sh
set -eu
: "${LIMEZU_DIR:?set LIMEZU_DIR to the folder that holds Modern_Interiors_v41.4/ and Modern_Exteriors_v42.3/}"
root=$(cd "$(dirname "$0")/../.." && pwd)
proj="$root/unity/AgentPaymentAuthority"
dest="$proj/Assets/LimeZu"
mkdir -p "$dest"
record="$dest/IMPORTED.txt"
: >"$record"
missing=0
grep -v '^#' "$proj/limezu-assets.txt" | while IFS="$(printf '\t')" read -r key rel rect; do
	[ -n "$key" ] || continue
	src="$LIMEZU_DIR/$rel"
	if [ ! -f "$src" ]; then
		echo "missing: $key $rel" >&2
		echo "MISSING $key $rel" >>"$record"
		continue
	fi
	mkdir -p "$dest/$(dirname "$rel")"
	cmp -s "$src" "$dest/$rel" || cp "$src" "$dest/$rel"
	printf '%s  %s  %s\n' "$(shasum -a 256 "$dest/$rel" | cut -d' ' -f1)" "$key" "$rel" >>"$record"
done
cat "$record"
if grep -q '^MISSING' "$record"; then
	echo "some files are missing; the scene builder uses placeholders for them" >&2
	exit 3
fi
