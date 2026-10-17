set shell := ["bash", "-euo", "pipefail", "-c"]

# Fast inner loop. CHECK_ALL=1 checks the full tree instead of the changed packages.
check:
    #!/usr/bin/env bash
    set -euo pipefail
    for c in checks/*.sh; do bash "$c" || { echo "FAIL: $c"; exit 1; }; done
    unformatted=$(gofmt -l cmd internal 2>/dev/null || true)
    if [ -n "$unformatted" ]; then echo "gofmt needed:"; echo "$unformatted"; exit 1; fi
    if [ "${CHECK_ALL:-}" = "1" ]; then
      pkgs=./...
    else
      base=$(git merge-base HEAD origin/main 2>/dev/null || echo HEAD)
      dirs=$( { git diff --name-only "$base"; git ls-files --others --exclude-standard; } | grep '\.go$' | xargs -r -n1 dirname | sort -u | while read -r d; do [ -d "$d" ] && echo "./$d"; done || true)
      pkgs=${dirs:-}
    fi
    go vet ./...
    if [ -n "$pkgs" ]; then go test $pkgs; fi

# Full suite with the race detector.
test:
    go test -race -count=1 ./...
