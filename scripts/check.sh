#!/usr/bin/env bash
# Lint and security gates. CI runs each one as a separate job; run them all
# locally with:  nix develop .#ci -c scripts/check.sh
#
# Usage: scripts/check.sh [all|go|vuln|workflows|shell|secrets|nix]...
set -euo pipefail
cd "$(dirname "$0")/.."

go_lint() {
  echo "== golangci-lint (gofmt, vet, staticcheck, gosec, errcheck, bidichk, ...)"
  golangci-lint run ./...
}

vuln() {
  echo "== govulncheck: no known-vulnerable code reachable"
  govulncheck ./...
}

workflows() {
  echo "== actionlint"
  actionlint
  echo "== zizmor: GitHub Actions security (pinning, permissions, injection, ...)"
  zizmor --min-severity low .github/workflows .github/dependabot.yml
}

shell() {
  echo "== shellcheck"
  shellcheck scripts/*.sh demo/*.sh site/*.sh
}

secrets() {
  echo "== gitleaks: no credentials anywhere in git history"
  gitleaks git --redact --no-banner .
}

nix_fmt() {
  echo "== nixfmt"
  nixfmt --check flake.nix nix/*.nix
}

targets=("$@")
[[ ${#targets[@]} -eq 0 || ${targets[0]} == all ]] && targets=(go vuln workflows shell secrets nix)
for t in "${targets[@]}"; do
  case "$t" in
  go) go_lint ;;
  vuln) vuln ;;
  workflows) workflows ;;
  shell) shell ;;
  secrets) secrets ;;
  nix) nix_fmt ;;
  *)
    echo "unknown check: $t" >&2
    exit 2
    ;;
  esac
done
echo "all requested checks passed"
