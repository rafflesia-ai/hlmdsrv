#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PKG="github.com/rafflesia-ai/hlmdsrv/internal/mdsrvcli"

# Version comes from the nearest tag; goreleaser supplies its own ldflags on
# release builds, so this only has to be right for local builds.
VERSION="$(cd "$ROOT" && git describe --tags --always --dirty 2>/dev/null || printf 'dev')"
COMMIT="$(cd "$ROOT" && git rev-parse --short=12 HEAD 2>/dev/null || true)"
DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

printf -- "-X %s.buildVersion=%s -X %s.buildCommit=%s -X %s.buildDate=%s" \
  "$PKG" "$VERSION" "$PKG" "$COMMIT" "$PKG" "$DATE"
