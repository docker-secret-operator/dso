#!/usr/bin/env bash
# Selftest for scripts/check-webui-assets.sh (no Node, no network).
# Builds a throw-away git repository with a tiny committed "asset" tree, then
# proves the checker passes on an exact match and FAILS for each drift class,
# in particular a generated chunk hidden by .gitignore (the case the former
# `git status --porcelain` CI step could not see).
#
# Usage: scripts/check-webui-assets.selftest.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CHECK="${SCRIPT_DIR}/check-webui-assets.sh"
WORK="$(mktemp -d "${TMPDIR:-/tmp}/dso-webui-selftest.XXXXXX")"
trap 'rm -rf -- "$WORK"' EXIT

PASS=0; FAILED=0
A="internal/webui/assets"

make_repo() { # <name>: committed tree + identical "generated" tree
    local r="${WORK}/$1"
    mkdir -p "$r/$A/_next/static/chunks/app/secrets" "$WORK/gen-$1/_next/static/chunks/app/secrets"
    git -C "$WORK" init -q "$r"
    printf 'secrets/\n' > "$r/.gitignore"
    printf '!%s/_next/static/chunks/app/secrets/\n' "$A" >> "$r/.gitignore"
    for base in "$r/$A" "$WORK/gen-$1"; do
        printf '<html><script src="/_next/static/chunks/a.js"></script><script src="/_next/static/chunks/app/secrets/page.js"></script></html>' > "$base/index.html"
        echo 'a' > "$base/_next/static/chunks/a.js"
        echo 'p' > "$base/_next/static/chunks/app/secrets/page.js"
    done
    git -C "$r" add -A
    git -C "$r" -c user.name=t -c user.email=t@t commit -q -m init
}

run() { DSO_CHECK_ROOT="${WORK}/$1" "$CHECK" --generated-dir "${WORK}/gen-$1" > "${WORK}/out-$1" 2>&1 && echo 0 || echo $?; }

expect() { # <name> <want-exit> <grep pattern or ''>
    local got; got="$(run "$1")"
    if [ "$got" = "$2" ] && { [ -z "$3" ] || grep -q -- "$3" "${WORK}/out-$1"; }; then
        PASS=$((PASS+1)); echo "ok   - $1"
    else
        FAILED=$((FAILED+1)); echo "FAIL - $1 (exit ${got}, want $2, pattern '$3')"; sed 's/^/       /' "${WORK}/out-$1"
    fi
}

make_repo clean;          expect clean 0 'matches a fresh build'

make_repo ignored-hidden
# The scenario the old CI step missed: a generated chunk under an ignored dir
# (re-ignore the directory so the exception no longer applies).
printf 'secrets/\n' > "${WORK}/ignored-hidden/.gitignore"
git -C "${WORK}/ignored-hidden" rm -q --cached -r "$A/_next/static/chunks/app/secrets"
git -C "${WORK}/ignored-hidden" -c user.name=t -c user.email=t@t commit -q -am hide
# `git status` is blind to it...
test -z "$(git -C "${WORK}/ignored-hidden" status --porcelain "$A")" || { echo "fixture error: git status sees it"; exit 1; }
expect ignored-hidden 1 'IGNORED by .gitignore'
expect ignored-hidden 1 'app/secrets/page.js'

make_repo modified;       echo 'changed' > "${WORK}/gen-modified/_next/static/chunks/a.js"; expect modified 1 'modified tracked'
make_repo missing-tracked; echo 'n' > "${WORK}/gen-missing-tracked/_next/static/chunks/new.js"; expect missing-tracked 1 'missing tracked'
make_repo obsolete;       rm "${WORK}/gen-obsolete/_next/static/chunks/a.js"; expect obsolete 1 'obsolete committed'
make_repo untracked;      echo x > "${WORK}/untracked/$A/_next/static/chunks/extra.js"; expect untracked 1 'untracked on disk'
make_repo disk-edited;    echo y > "${WORK}/disk-edited/$A/_next/static/chunks/a.js"; expect disk-edited 1 'differs from the committed'
make_repo disk-deleted;   rm "${WORK}/disk-deleted/$A/_next/static/chunks/a.js"; expect disk-deleted 1 'tracked but absent on disk'
make_repo symlink;        ln -s a.js "${WORK}/symlink/$A/_next/static/chunks/l.js"; expect symlink 1 'symlinks or non-regular'

make_repo dangling
# html references a chunk that exists nowhere (committed, generated or on disk)
for base in "${WORK}/dangling/$A" "${WORK}/gen-dangling"; do
    printf '<html><script src="/_next/static/chunks/gone.js"></script></html>' > "$base/index.html"
done
git -C "${WORK}/dangling" -c user.name=t -c user.email=t@t commit -q -am dangling
expect dangling 1 'dangling /_next/static reference'

# Source archive (no .git): generated vs on-disk only.
make_repo archive; rm -rf "${WORK}/archive/.git"; expect archive 0 'not a git work tree'
make_repo archive-drift; rm -rf "${WORK}/archive-drift/.git"; echo z > "${WORK}/gen-archive-drift/_next/static/chunks/a.js"; expect archive-drift 1 'differs from a fresh build'

echo ""
echo "check-webui-assets selftest: ${PASS} passed, ${FAILED} failed"
[ "$FAILED" -eq 0 ]
