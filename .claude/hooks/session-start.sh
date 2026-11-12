#!/bin/bash
# SessionStart hook for Claude Code on the web (Go profile): downloads the modules so the squad can
# format, vet, lint, test and check coverage. Idempotent; does nothing outside remote sessions.
set -euo pipefail

if [[ "${CLAUDE_CODE_REMOTE:-}" != "true" ]]; then
  exit 0
fi

# Linters for the analyzer gate (shellcheck, actionlint, hadolint), pinned and checksum-verified; never fails the session.
bash "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/install-linters.sh"

cd "${CLAUDE_PROJECT_DIR:-$(pwd)}"

go mod download

# The analyzer gate needs the golangci-lint release pinned in CI, built with the Go version that go.mod
# targets; a stock or older binary refuses to run. Install it once into $HOME/go/bin and put that on PATH.
want="$(sed -n 's/^ *version: *\(v[0-9][0-9.]*\) *$/\1/p' .github/workflows/ci.yml 2>/dev/null | head -n 1 || true)"
have="$(golangci-lint version --short 2>/dev/null || true)"
if [[ -n "$want" && "v${have#v}" != "$want" ]]; then
  GOTOOLCHAIN="$(go env GOVERSION)" go install "github.com/golangci/golangci-lint/v2/cmd/golangci-lint@${want}" \
    || echo "golangci-lint ${want} could not be installed; the analyzer gate needs it (see .squad/stack.md, Known pitfalls)." >&2
  if [[ -n "${CLAUDE_ENV_FILE:-}" ]]; then
    echo "export PATH=\"$(go env GOPATH)/bin:\$PATH\"" >> "$CLAUDE_ENV_FILE"
  fi
elif ! command -v golangci-lint >/dev/null 2>&1; then
  echo "golangci-lint is not installed; the analyzer gate needs it (see .squad/stack.md, Known pitfalls)." >&2
fi
