#!/usr/bin/env bash
# check: no-private-docs
# born: 2026-10-17
# failure: a private build file or its marker reaches the public repo
# rule: tracked files never live under .build/ and never contain the private marker
set -euo pipefail
marker="Internal build"" doc"
bad=$(git ls-files | grep -E '^(\.build/|CLAUDE\.local\.md$)' || true)
if [ -n "$bad" ]; then echo "private files are tracked:"; echo "$bad"; exit 1; fi
hits=$(git grep -l -F "$marker" -- . ':!checks/no-private-docs.sh' || true)
if [ -n "$hits" ]; then echo "private marker found in:"; echo "$hits"; exit 1; fi
