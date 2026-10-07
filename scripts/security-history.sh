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

cve='cve-[0-9]{4}-[0-9]+'
ghsa='ghsa-[a-z0-9]{4}-[a-z0-9]{4}-[a-z0-9]{4}'
security='(^|[^a-z0-9_])security([^a-z0-9_]|$)'
harden='(^|[^a-z0-9_])harden'

# git log --grep would match the whole message; match the subject and the Refs
# trailer separately so a passing mention in a body does not count.
git log --no-merges \
    --format='%h%x1f%s%x1f%(trailers:key=Refs,valueonly,unfold,separator=%x20)' \
    -- "$@" |
    while IFS=$'\x1f' read -r sha subject refs; do
        s=${subject,,}
        r=${refs,,}
        if [[ $s =~ $cve || $s =~ $ghsa || $s =~ $security || $s =~ $harden || $r =~ $ghsa ]]; then
            printf '%s %s\n' "$sha" "$subject"
        fi
    done
