#!/usr/bin/env bash
# Lists earlier commits on the given paths that fixed a security defect, so an
# orchestrate dispatch can tell the executor and verifier which guards not to
# undo. A commit counts when its subject names a CVE or GHSA id or says
# security/harden, or its body carries a `Refs: GHSA-…` trailer.
#
# Usage: scripts/security-history.sh <path>...
# Prints one line per commit: "<short sha> <subject>". Empty output means none.
set -euo pipefail

if [[ $# -eq 0 ]]; then
    echo "usage: scripts/security-history.sh <path>..." >&2
    exit 2
fi

git rev-parse --git-dir >/dev/null

git log --no-merges --format='%h %s' -i -E \
    --grep='CVE-[0-9]{4}-[0-9]+' \
    --grep='GHSA-[a-z0-9]{4}-[a-z0-9]{4}-[a-z0-9]{4}' \
    --grep='\bsecurity\b' \
    --grep='\bharden' \
    -- "$@"
