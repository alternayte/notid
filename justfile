set shell := ["bash", "-euo", "pipefail", "-c"]

export TEST_DATABASE_URL := env("TEST_DATABASE_URL", "postgres://notid:notid@localhost:55433/notid_test?sslmode=disable")

# Start the test Postgres container once; later calls reuse it.
db:
    #!/usr/bin/env bash
    set -euo pipefail
    if [ -n "${CI:-}" ]; then exit 0; fi
    if ! docker ps --format '{{{{.Names}}' | grep -qx notid-test-pg; then
      docker rm -f notid-test-pg >/dev/null 2>&1 || true
      docker run -d --name notid-test-pg -e POSTGRES_USER=notid -e POSTGRES_PASSWORD=notid \
        -e POSTGRES_DB=notid_test -p 55433:5432 postgres:17 >/dev/null
    fi
    for _ in $(seq 1 60); do
      docker exec notid-test-pg pg_isready -U notid -d notid_test >/dev/null 2>&1 && exit 0
      sleep 0.5
    done
    echo "test database did not start"; exit 1

# Fast inner loop. CHECK_ALL=1 checks the full tree instead of the changed packages.
check: db
    #!/usr/bin/env bash
    set -euo pipefail
    for c in checks/*.sh; do bash "$c" || { echo "FAIL: $c"; exit 1; }; done
    unformatted=$(gofmt -l cmd internal migrations 2>/dev/null || true)
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
test: db
    go test -race -count=1 ./...
