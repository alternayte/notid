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
    unformatted=$(gofmt -l cmd internal migrations interop 2>/dev/null || true)
    if [ -n "$unformatted" ]; then echo "gofmt needed:"; echo "$unformatted"; exit 1; fi
    if [ "${CHECK_ALL:-}" = "1" ]; then
      pkgs=./...
    else
      base=$(git merge-base HEAD origin/main 2>/dev/null || echo HEAD)
      dirs=$( { git diff --name-only "$base"; git ls-files --others --exclude-standard; } | grep '\.go$' | xargs -r -n1 dirname | sort -u | while read -r d; do [ -d "$d" ] && [ "$d" != "interop" ] && echo "./$d"; done || true)
      pkgs=${dirs:-}
    fi
    sqlc generate
    if ! git diff --quiet -- internal/*/*db || [ -n "$(git ls-files --others --exclude-standard -- 'internal/*/*db')" ]; then
      if [ -n "${CI:-}" ]; then echo "sqlc output is stale; run sqlc generate and commit"; git status --short -- internal; exit 1; fi
    fi
    go vet ./...
    go vet -tags interop ./interop/...
    if [ -n "$pkgs" ]; then go test $pkgs; fi
    go run ./cmd/notid openapi > web/src/api/openapi.json
    (cd web && bun install --frozen-lockfile --silent && bun run --silent gen:api >/dev/null)
    if [ -n "${CI:-}" ] && [ -n "$(git status --porcelain -- web/src/api)" ]; then
      echo "the API client is stale; run just check and commit web/src/api"; git diff --stat -- web/src/api; exit 1
    fi
    if [ "${CHECK_ALL:-}" = "1" ] || [ -n "$( { git diff --name-only "${base:-HEAD}"; git ls-files --others --exclude-standard; } | grep '^web/' || true)" ]; then
      (cd web && bun run --silent typecheck && bun run --silent lint && bun run --silent test)
    fi

# Run the web dev server on every interface and print its tailnet URL, for a phone on Tailscale.
web-dev-tailnet port="3002":
    #!/usr/bin/env bash
    set -euo pipefail
    ts=$(command -v tailscale || echo /Applications/Tailscale.app/Contents/MacOS/Tailscale)
    ip=$("$ts" ip -4 2>/dev/null | head -n1 || true)
    echo "Open http://${ip:-<tailscale-ip>}:{{port}}/ on any device in the tailnet."
    cd web && bun --bun vite dev --port {{port}} --strictPort --host 0.0.0.0

# Full suite with the race detector, and every web test.
test: db
    go test -race -count=1 ./...
    cd web && bun install --frozen-lockfile --silent && bun run test

# Run the web app in dev mode behind nothing; use `notid serve` with WEB_UPSTREAM=http://127.0.0.1:3000 in front.
web-dev:
    cd web && bun run dev

# Interop harness. The first run builds Lemmy from source in debug mode.
interop:
    #!/usr/bin/env bash
    set -euo pipefail
    bash interop/lemmy/build.sh
    docker compose -f interop/compose.yaml down -v --remove-orphans >/dev/null 2>&1 || true
    mkdir -p interop/.run
    cp interop/coredns/hosts interop/.run/hosts
    docker compose -f interop/compose.yaml up -d --build
    bash interop/wait.sh
    go test -tags interop -count=1 -timeout 45m ./interop/...

interop-down:
    docker compose -f interop/compose.yaml down

interop-clean:
    docker compose -f interop/compose.yaml down -v --remove-orphans
