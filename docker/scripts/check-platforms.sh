#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
builder="${BUILDER:-npu-builder}"
image="${TEST_IMAGE:-emeet-pixy-dual-cam-platform-check}"
desktop_tools="${INSTALL_DESKTOP_TOOLS:-0}"
platforms=(linux/amd64 linux/arm64 linux/arm/v7)
tags=("${image}:amd64" "${image}:arm64" "${image}:armv7")

cleanup() {
  docker image rm -f "${tags[@]}" >/dev/null 2>&1 || true
}
trap cleanup EXIT

for target in "${platforms[@]}"; do
  case "$target" in
    linux/amd64) suffix=amd64 ;;
    linux/arm64) suffix=arm64 ;;
    linux/arm/v7) suffix=armv7 ;;
    *) printf 'Unsupported platform: %s\n' "$target" >&2; exit 1 ;;
  esac
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
    # registered for ARM targets. The Dockerfile smoke stage executes the
    # target binaries in BuildKit's QEMU runner.
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

printf '\nAll target architectures passed the runtime smoke stage.\n'
printf 'ARM checks use BuildKit/QEMU emulation; they are not physical Raspberry Pi tests.\n'
