#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
builder="${BUILDER:-npu-builder}"
image="${TEST_IMAGE:-emeet-pixy-dual-cam-platform-check}"
desktop_tools="${INSTALL_DESKTOP_TOOLS:-0}"
tags=("${image}:amd64" "${image}:arm64")

cleanup() {
  docker image rm -f "${tags[@]}" >/dev/null 2>&1 || true
}
trap cleanup EXIT

for target in linux/amd64 linux/arm64; do
  suffix="${target##*/}"
  tag="${image}:${suffix}"
  printf '\n== Build and smoke-test %s ==\n' "$target"

  if [[ "$target" == linux/amd64 ]]; then
    docker buildx build \
      --builder "$builder" \
      --platform "$target" \
      --load \
      --build-arg VERSION=platform-check \
      --build-arg INSTALL_DESKTOP_TOOLS="$desktop_tools" \
      --tag "$tag" \
      --file "$repo_root/docker/Dockerfile" \
      "$repo_root"

    docker run --rm --platform "$target" "$tag" --version
  else
    # The Docker Engine on an amd64 workstation may not have binfmt/QEMU
    # registered for `docker run --platform linux/arm64`. The Dockerfile's
    # smoke stage executes the actual ARM64 runtime binaries inside BuildKit's
    # QEMU runner without installing host-wide binfmt handlers.
    docker buildx build \
      --builder "$builder" \
      --platform "$target" \
      --target smoke \
      --output=type=cacheonly \
      --build-arg VERSION=platform-check \
      --build-arg INSTALL_DESKTOP_TOOLS="$desktop_tools" \
      --file "$repo_root/docker/Dockerfile" \
      "$repo_root"
  fi
done

printf '\nBoth target architectures passed the runtime smoke stage.\n'
printf 'The arm64 check uses BuildKit/QEMU emulation; it is not a physical Raspberry Pi test.\n'
