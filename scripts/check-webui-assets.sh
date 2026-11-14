#!/usr/bin/env bash
# ==============================================================================
# DSO Web UI embedded-asset drift check
# ==============================================================================
# Verifies that internal/webui/assets/ -- the committed, go:embed'ed build of
# web/ -- is exactly what scripts/build-webui.sh produces from the current
# source, WITHOUT relying on `git status` (which cannot see ignored files).
#
# It builds a fresh tree into a scratch directory with the same procedure
# (scripts/build-webui.sh; the checked-in tree is never touched) and compares
# three inventories of "relative path + sha256" (modes are ignored):
#
#   G  generated   the fresh build
#   T  tracked     what git has committed under internal/webui/assets (read
#                  from the HEAD tree objects; --index reads the index)
#   D  disk        every regular file actually present on disk under
#                  internal/webui/assets, INCLUDING git-ignored ones (find)
#
# and fails (exit 1) on any of:
#   - modified tracked      path in T and G, different content
#   - missing tracked       path in G but not in T (generated, never committed)
#   - obsolete committed    path in T but not in G
#   - on-disk modified      path in D and T, different content
#   - tracked, not on disk  path in T but not in D
#   - IGNORED not tracked   path in D, ignored by git, not in T (the class
#                           `git status` cannot see: a .gitignore rule hides a
#                           generated file, so a fresh clone lacks it)
#   - untracked on disk     path in D, not ignored, not in T
#   - unexpected extra      path in D that is not in G
#   - dangling reference    a /_next/static/... URL in any committed .html/.txt
#                           that exists in neither T nor D, or not in G
#   - symlinks or non-regular files anywhere in the compared trees
#
# Usage: scripts/check-webui-assets.sh [--index] [--generated-dir DIR]
#   --index              treat the git index (not HEAD) as the tracked set
#   --generated-dir DIR  compare against an already generated tree instead of
#                        building one (used by the selftest)
# Environment: DSO_CHECK_ROOT overrides the repository root (selftest only).
# Exit: 0 identical; 1 drift detected; 2 usage/environment error.
#
# Without a .git directory (source archive) only G-vs-D can be evaluated; the
# script says so and still fails on any difference.
# ==============================================================================

set -euo pipefail
export LC_ALL=C

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="${DSO_CHECK_ROOT:-$(dirname "$SCRIPT_DIR")}"
ASSETS_REL="internal/webui/assets"
ASSETS_DIR="${PROJECT_ROOT}/${ASSETS_REL}"

USE_INDEX=0
GEN_DIR=""
while [ $# -gt 0 ]; do
    case "$1" in
        --index) USE_INDEX=1 ;;
        --generated-dir)
            [ $# -ge 2 ] || { echo "Error: --generated-dir needs a value" >&2; exit 2; }
            GEN_DIR="$2"; shift ;;
        *) echo "Error: unknown argument: $1" >&2; exit 2 ;;
    esac
    shift
done

command -v sha256sum >/dev/null 2>&1 || { echo "Error: sha256sum not found" >&2; exit 2; }
[ -d "$ASSETS_DIR" ] || { echo "Error: ${ASSETS_DIR} does not exist" >&2; exit 2; }

# ── scratch space (only ever removes the directory this run created) ────────
SCRATCH="$(mktemp -d "${TMPDIR:-/tmp}/dso-webui-check.XXXXXX")"
MARKER="${SCRATCH}/.dso-webui-check-owner"
: > "$MARKER"
cleanup() {
    if [ -n "${SCRATCH:-}" ] && [ -d "$SCRATCH" ] && [ ! -L "$SCRATCH" ] && [ -f "$MARKER" ]; then
        rm -rf -- "$SCRATCH"
    fi
}
trap cleanup EXIT

# ── generated tree ──────────────────────────────────────────────────────────
if [ -z "$GEN_DIR" ]; then
    GEN_DIR="${SCRATCH}/generated"
    echo "Generating a fresh Web UI build into a scratch directory (scripts/build-webui.sh)..."
    DSO_WEBUI_OUT_DIR="$GEN_DIR" "${SCRIPT_DIR}/build-webui.sh" >"${SCRATCH}/build.log" 2>&1 || {
        echo "Error: scripts/build-webui.sh failed; last lines of its output:" >&2
        tail -n 40 "${SCRATCH}/build.log" >&2
        exit 2
    }
    echo "Fresh build generated."
fi
[ -d "$GEN_DIR" ] || { echo "Error: generated dir ${GEN_DIR} missing" >&2; exit 2; }

# ── inventories: lines of "<path>\t<sha256>", sorted by path ────────────────
inventory_dir() { # <dir> <outfile>; fails on symlinks / odd names
    local dir="$1" out="$2" f rel
    : > "$out"
    while IFS= read -r -d '' f; do
        rel="${f#"$dir"/}"
        case "$rel" in *$'\t'*|*$'\n'*) echo "Error: unsupported file name in ${dir}: ${rel}" >&2; exit 2 ;; esac
        if [ -L "$f" ] || [ ! -f "$f" ]; then
            printf '%s\t%s\n' "$rel" "NON-REGULAR(symlink-or-special)" >> "$out"
        else
            printf '%s\t%s\n' "$rel" "$(sha256sum < "$f" | cut -d' ' -f1)" >> "$out"
        fi
    done < <(find "$dir" \( -type f -o -type l -o -type p -o -type s -o -type b -o -type c \) -print0)
    sort -t $'\t' -k1,1 -o "$out" "$out"
}

HAVE_GIT=0
if command -v git >/dev/null 2>&1 && git -C "$PROJECT_ROOT" rev-parse --is-inside-work-tree >/dev/null 2>&1 \
    && [ "$(git -C "$PROJECT_ROOT" rev-parse --show-toplevel)" = "$(cd "$PROJECT_ROOT" && pwd -P)" ]; then
    HAVE_GIT=1
fi

G="${SCRATCH}/G.tsv"; D="${SCRATCH}/D.tsv"; T="${SCRATCH}/T.tsv"
inventory_dir "$GEN_DIR" "$G"
inventory_dir "$ASSETS_DIR" "$D"

TRACKED_LABEL="HEAD"
if [ "$HAVE_GIT" = "1" ]; then
    : > "$T"
    if [ "$USE_INDEX" = "1" ]; then
        TRACKED_LABEL="index"
        listing="$(git -C "$PROJECT_ROOT" ls-files -s -z -- "$ASSETS_REL" | tr '\0' '\n')"
    else
        listing="$(git -C "$PROJECT_ROOT" ls-tree -r -z HEAD -- "$ASSETS_REL" 2>/dev/null | tr '\0' '\n' || true)"
    fi
    # ls-tree: "<mode> blob <oid>\t<path>"   ls-files -s: "<mode> <oid> <stage>\t<path>"
    while IFS= read -r line; do
        [ -n "$line" ] || continue
        meta="${line%%$'\t'*}"; path="${line#*$'\t'}"
        mode="${meta%% *}"
        if [ "$USE_INDEX" = "1" ]; then oid="$(echo "$meta" | awk '{print $2}')"; else oid="$(echo "$meta" | awk '{print $3}')"; fi
        rel="${path#"${ASSETS_REL}"/}"
        if [ "$mode" != "100644" ] && [ "$mode" != "100755" ]; then
            printf '%s\t%s\n' "$rel" "NON-REGULAR(mode ${mode})" >> "$T"
        else
            printf '%s\t%s\n' "$rel" "$(git -C "$PROJECT_ROOT" cat-file blob "$oid" | sha256sum | cut -d' ' -f1)" >> "$T"
        fi
    done <<< "$listing"
    sort -t $'\t' -k1,1 -o "$T" "$T"
    if [ ! -s "$T" ]; then
        echo "Note: no tracked files under ${ASSETS_REL} in ${TRACKED_LABEL}; every generated file is therefore 'missing tracked'." >&2
    fi
    # git's own view of what is ignored / untracked on disk under assets/
    git -C "$PROJECT_ROOT" ls-files -z --others --ignored --exclude-standard -- "$ASSETS_REL" \
        | tr '\0' '\n' | sed "s#^${ASSETS_REL}/##" | LC_ALL=C sort > "${SCRATCH}/ignored.txt"
    git -C "$PROJECT_ROOT" ls-files -z --others --exclude-standard -- "$ASSETS_REL" \
        | tr '\0' '\n' | sed "s#^${ASSETS_REL}/##" | LC_ALL=C sort > "${SCRATCH}/untracked.txt"
else
    echo "Note: ${PROJECT_ROOT} is not a git work tree (source archive?); tracked-set checks skipped, comparing generated vs on-disk only." >&2
fi

FAIL=0
section() { # <title> <file with lines>  -> prints and marks failure when non-empty
    if [ -s "$2" ]; then
        FAIL=1
        echo ""
        echo "DRIFT: $1"
        sed 's/^/    /' "$2"
    fi
}
paths() { cut -f1 "$1"; }
join_diff() { # paths in both whose hash differs: <A> <B> -> "path (A:short B:short)"
    join -t $'\t' -j1 "$1" "$2" | awk -F'\t' '$2!=$3 {printf "%s  (%s != %s)\n",$1,substr($2,1,12),substr($3,1,12)}'
}
only_in() { # paths in A not in B
    comm -23 <(paths "$1") <(paths "$2")
}

N_G="$(wc -l < "$G")"; N_D="$(wc -l < "$D")"
echo ""
echo "Inventory: generated=${N_G} files, on-disk=${N_D} files$( [ "$HAVE_GIT" = 1 ] && echo ", tracked(${TRACKED_LABEL})=$(wc -l < "$T") files" )"

if [ "$HAVE_GIT" = "1" ]; then
    join_diff "$T" "$G" > "${SCRATCH}/r1"; section "modified tracked (committed content differs from a fresh build)" "${SCRATCH}/r1"
    only_in "$G" "$T"  > "${SCRATCH}/r2"; section "missing tracked (generated by the build but not committed)" "${SCRATCH}/r2"
    only_in "$T" "$G"  > "${SCRATCH}/r3"; section "obsolete committed (committed but no longer generated)" "${SCRATCH}/r3"
    join_diff "$T" "$D" > "${SCRATCH}/r4"; section "on-disk copy differs from the committed content" "${SCRATCH}/r4"
    only_in "$T" "$D"  > "${SCRATCH}/r5"; section "tracked but absent on disk" "${SCRATCH}/r5"
    only_in "$D" "$T" > "${SCRATCH}/not-tracked.txt"
    comm -12 "${SCRATCH}/not-tracked.txt" "${SCRATCH}/ignored.txt" > "${SCRATCH}/r6"
    section "IGNORED by .gitignore and not tracked (invisible to git status; absent from a fresh clone)" "${SCRATCH}/r6"
    comm -12 "${SCRATCH}/not-tracked.txt" "${SCRATCH}/untracked.txt" > "${SCRATCH}/r7"
    section "untracked on disk" "${SCRATCH}/r7"
fi
join_diff "$D" "$G" > "${SCRATCH}/r8"; section "on-disk file differs from a fresh build" "${SCRATCH}/r8"
only_in "$D" "$G"  > "${SCRATCH}/r9"; section "unexpected extra on disk (not produced by the build)" "${SCRATCH}/r9"
only_in "$G" "$D"  > "${SCRATCH}/r10"; section "generated but absent on disk" "${SCRATCH}/r10"
INV_FILES=("$G" "$D")
if [ "$HAVE_GIT" = "1" ]; then INV_FILES+=("$T"); fi
grep -F 'NON-REGULAR' "${INV_FILES[@]}" > "${SCRATCH}/r11" 2>/dev/null || true
section "symlinks or non-regular files in an asset tree" "${SCRATCH}/r11"

# ── every /_next/static/... URL referenced by a committed page must exist ───
# Referenced URLs are taken from the on-disk html/txt (what is embedded) and
# must exist in the generated set, the on-disk set and (with git) the tracked set.
: > "${SCRATCH}/refs.txt"
while IFS= read -r -d '' f; do
    grep -aoE '/_next/static/[A-Za-z0-9_./~@+=%-]+\.(js|css|woff2?|ttf|otf|map|json|svg|png|ico)' "$f" 2>/dev/null \
        | sed 's#^/##' | sort -u | sed "s#\$#\t${f#"$ASSETS_DIR"/}#" >> "${SCRATCH}/refs.txt" || true
done < <(find "$ASSETS_DIR" -type f \( -name '*.html' -o -name '*.txt' \) -print0)
# Reference URLs lose the leading "/", so they are bare "_next/static/..." paths.
paths "$G" > "${SCRATCH}/PG"; paths "$D" > "${SCRATCH}/PD"
if [ "$HAVE_GIT" = "1" ]; then paths "$T" > "${SCRATCH}/PT"; fi
: > "${SCRATCH}/r12"
while IFS=$'\t' read -r ref src; do
    [ -n "$ref" ] || continue
    miss=""
    grep -qxF -- "$ref" "${SCRATCH}/PG" || miss="${miss} generated"
    grep -qxF -- "$ref" "${SCRATCH}/PD" || miss="${miss} on-disk"
    if [ "$HAVE_GIT" = "1" ]; then grep -qxF -- "$ref" "${SCRATCH}/PT" || miss="${miss} tracked(${TRACKED_LABEL})"; fi
    [ -z "$miss" ] || printf '%s  referenced by %s  missing from:%s\n' "$ref" "$src" "$miss" >> "${SCRATCH}/r12"
done < "${SCRATCH}/refs.txt"
sort -u -o "${SCRATCH}/r12" "${SCRATCH}/r12"
section "dangling /_next/static reference" "${SCRATCH}/r12"

echo ""
if [ "$FAIL" -ne 0 ]; then
    echo "❌ ${ASSETS_REL}/ does not match a fresh build of web/ (see DRIFT sections above)."
    echo "   Run 'make webui-build' (scripts/build-webui.sh) locally with the Node version in web/.nvmrc,"
    echo "   then commit ALL of ${ASSETS_REL}/ (check 'git status --ignored ${ASSETS_REL}' for hidden files)."
    exit 1
fi
echo "✅ ${ASSETS_REL}/ matches a fresh build of web/ (${N_G} files; tracked, on-disk and generated inventories identical)."
