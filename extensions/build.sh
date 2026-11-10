#!/bin/sh
# Packages the extensions for the stores: one zip each, with the manifest at
# the root and no listing assets inside.
set -eu

cd "$(dirname "$0")/.."
out="dist"
mkdir -p "$out"

# The two extensions share their code; the manifests are what differ.
for f in background.js popup.html popup.js; do
	if ! cmp -s "extensions/chrome/$f" "extensions/firefox/$f"; then
		echo "extensions: $f differs between chrome and firefox" >&2
		exit 1
	fi
done

version=$(sed -n 's/.*"version": *"\([^"]*\)".*/\1/p' extensions/chrome/manifest.json | head -n 1)
if [ -z "$version" ]; then
	echo "extensions: no version in the manifest" >&2
	exit 1
fi

for browser in chrome firefox; do
	zip="$out/downmygo-$browser-$version.zip"
	rm -f "$zip"
	(cd "extensions/$browser" && zip -qr "../../$zip" . \
		-x "README.md" -x "store/*" -x ".DS_Store" -x "*/__MACOSX/*")
	echo "$zip"
done
