#!/usr/bin/env bash
# Waits until every harness host answers on its published loopback port.
set -euo pipefail

wait_for() {
  local name="$1" url="$2"
  for _ in $(seq 1 300); do
    if curl -fsS -o /dev/null --max-time 2 "${url}" 2>/dev/null; then
      return 0
    fi
    sleep 1
  done
  echo "timed out waiting for ${name} at ${url}" >&2
  return 1
}

wait_for "notid A" "http://127.0.0.1:28080/healthz"
wait_for "notid B" "http://127.0.0.1:28081/healthz"
wait_for "Lemmy" "http://127.0.0.1:28536/api/v3/site"
wait_for "PieFed" "http://127.0.0.1:25000/health"
wait_for "mailpit" "http://127.0.0.1:28025/livez"
