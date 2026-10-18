#!/bin/sh
# Prints the body of the CHANGELOG.md section for one version, "## [0.1.0] - 2026-11-02" style.
# Exits 1 when the section is missing or empty.
set -eu
version="$1"
file="${2:-CHANGELOG.md}"
body=$(awk -v v="$version" '
  /^## \[/ { if (found) exit; if (index($0, "## [" v "]") == 1) { found = 1; next } }
  found { print }
' "$file")
if [ -z "$(printf '%s' "$body" | tr -d '[:space:]')" ]; then
  echo "changelog-section: no notes for $version in $file" >&2
  exit 1
fi
printf '%s\n' "$body"
