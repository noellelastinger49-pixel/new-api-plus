#!/usr/bin/env bash
#
# Build new-api from source and export the image to deploy/prod/images/new-api.tar.
#
# Strategy:
#   1. Build frontend (default + classic) locally with native bun — avoids QEMU
#      tarball extraction failures when cross-compiling for linux/amd64 on ARM Mac.
#   2. Build Go binary inside Docker for the target platform (cross-compilation).
#   3. Save the final image to images/new-api.tar.
#
# Usage:
#   ./build-new-api.sh              Build for linux/amd64 (default)
#   ./build-new-api.sh --arm64      Build for linux/arm64
#   ./build-new-api.sh --no-cache   Build without Docker layer cache
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
IMAGE_NAME="new-api:prod-local"
IMAGE_TAR="${SCRIPT_DIR}/images/new-api.tar"
PLATFORM="linux/amd64"
GOARCH="amd64"
NO_CACHE=""

log() {
  printf '[build] %s\n' "$*"
}

die() {
  printf '[build] ERROR: %s\n' "$*" >&2
  exit 1
}

for arg in "$@"; do
  case "$arg" in
    --arm64)    PLATFORM="linux/arm64"; GOARCH="arm64" ;;
    --no-cache) NO_CACHE="--no-cache" ;;
    -h|--help)
      cat <<EOF
Usage: $0 [options]

Options:
  --arm64      Build for linux/arm64 instead of linux/amd64
  --no-cache   Disable Docker build cache
  -h, --help   Show this help message
EOF
      exit 0
      ;;
    *) die "Unknown option: ${arg}. Run '$0 --help' for usage." ;;
  esac
done

command -v docker >/dev/null 2>&1 || die "docker is required but not installed"
command -v bun >/dev/null 2>&1    || die "bun is required but not installed (https://bun.sh)"

VERSION="$(cat "${PROJECT_ROOT}/VERSION")"

log "Project root : ${PROJECT_ROOT}"
log "Version      : ${VERSION}"
log "Target image : ${IMAGE_NAME}"
log "Platform     : ${PLATFORM}"
log "Output tar   : ${IMAGE_TAR}"
echo

# ── Step 1: Build frontend (default theme) ──────────────────────────────────
log "Step 1/4 — Installing web workspace dependencies..."
cd "${PROJECT_ROOT}/web"
bun install --frozen-lockfile

log "Step 2/4 — Building default frontend..."
cd "${PROJECT_ROOT}/web/default"
DISABLE_ESLINT_PLUGIN=true VITE_REACT_APP_VERSION="$VERSION" bun run build

log "Step 3/4 — Building classic frontend..."
cd "${PROJECT_ROOT}/web/classic"
VITE_REACT_APP_VERSION="$VERSION" bun run build

# ── Step 2: Build Docker image (Go binary only) ──────────────────────────────
cd "${PROJECT_ROOT}"
log "Step 4/4 — Building Docker image (Go binary, platform=${PLATFORM}, GOARCH=${GOARCH})..."
# shellcheck disable=SC2086
docker build \
  --platform "$PLATFORM" \
  --build-arg TARGETARCH="$GOARCH" \
  --build-arg TARGETOS="linux" \
  --file Dockerfile.local \
  --tag "$IMAGE_NAME" \
  $NO_CACHE \
  .

# ── Step 3: Export ───────────────────────────────────────────────────────────
log "Saving image to ${IMAGE_TAR}"
mkdir -p "${SCRIPT_DIR}/images"
docker save "$IMAGE_NAME" -o "$IMAGE_TAR"

log "Done."
log "  Image : ${IMAGE_NAME}"
log "  Tar   : ${IMAGE_TAR}"
echo
log "Next step: copy deploy/prod/ to the production server and run ./deploy.sh"
