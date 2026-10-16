#!/usr/bin/env bash
# Publish sdks/go to its public GitHub repository.
#
# The monorepo (GitLab, private) is the source of truth. This pushes ONLY the
# sdks/go subtree, with its history, to github.com/sendclipper/sendclipper-go.
# Never push the monorepo itself there.
#
#   sdks/go/scripts/split-to-github.sh            # push main
#   sdks/go/scripts/split-to-github.sh v0.1.0     # push main and tag v0.1.0
#
# A tag IS the release: `go get` reads the repository's tags through the Go
# module proxy. The proxy caches a version FOREVER the first time anyone
# fetches it, so a tag can never be moved or reused once pushed. Let CI go
# green on main first, then tag.
#
# REMOTE defaults to the `github-sendclipper` SSH alias (~/.ssh/config), which
# authenticates as the sendclipper GitHub account.
set -euo pipefail

REMOTE="${REMOTE:-git@github-sendclipper:sendclipper/sendclipper-go.git}"
TAG="${1:-}"
ROOT="$(git rev-parse --show-toplevel)"
cd "$ROOT"

if [ -n "$(git status --porcelain -- sdks/go)" ]; then
  echo "sdks/go has uncommitted changes; commit them first." >&2
  exit 1
fi

if [ -n "$TAG" ]; then
  version="$(sed -n 's/^const Version = "\(.*\)"/\1/p' sdks/go/version.go)"
  if [ "v$version" != "$TAG" ]; then
    echo "tag $TAG does not match Version $version" >&2
    exit 1
  fi
  if git ls-remote --exit-code --tags "$REMOTE" "refs/tags/$TAG" >/dev/null 2>&1; then
    echo "tag $TAG already exists on $REMOTE; Go versions are immutable, bump Version instead." >&2
    exit 1
  fi
fi

SPLIT="$(git subtree split --prefix=sdks/go HEAD 2>/dev/null)"
echo "split commit: $SPLIT"
git push "$REMOTE" "$SPLIT:refs/heads/main"

if [ -n "$TAG" ]; then
  git push "$REMOTE" "$SPLIT:refs/tags/$TAG"
fi
