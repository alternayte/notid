#!/usr/bin/env bash
# Builds the Lemmy used by the harness. Release builds refuse http URLs and
# private addresses, so this builds the default debug image from source once.
set -euo pipefail

tag="${LEMMY_TAG:-0.19.20}"
image="notid-interop-lemmy:${tag}"
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
src="${here}/.src"

if docker image inspect "${image}" >/dev/null 2>&1; then
  exit 0
fi

if [ ! -d "${src}/.git" ]; then
  git clone --depth 1 --branch "${tag}" --recurse-submodules \
    https://github.com/LemmyNet/lemmy.git "${src}"
fi

# 0.19.20 pairs an arm64 cross toolchain that needs glibc 2.38 with a bookworm
# runner that has 2.36, so the arm64 binary cannot start there.
docker build -f "${src}/docker/Dockerfile" \
  --build-arg ARM_RUNNER_IMAGE=debian:trixie-slim \
  -t "${image}" "${src}"
