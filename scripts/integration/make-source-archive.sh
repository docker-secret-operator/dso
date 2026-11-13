#!/usr/bin/env bash
# make-source-archive.sh -- build a history-free, deterministic source archive of ONE
# exact commit for transfer to the disposable test VM. Run it on the DEVELOPMENT host
# (no Docker, no network). The branch is not pushed and the VM never receives .git or
# history: `git clone` + `git checkout <sha>` on the VM is NOT a valid way to get the code.
#
#   scripts/integration/make-source-archive.sh <full-40-hex-sha> <output-dir>
#
# Produces in <output-dir> (must be outside the work tree, must not already contain the files):
#   dso-<sha12>.tar.gz   `git archive` of the commit tree only (fixed mtime = commit time,
#                        fixed umask, `gzip -n`), top-level directory dso-<sha12>/
#   provenance.json      sha, tree_sha (git rev-parse <sha>^{tree}), branch, commit time,
#                        creation time, tool versions, tarball name + sha256
#   SHA256SUMS           sha256 of the tarball and of provenance.json
#
# Refuses: dirty work tree (tracked or untracked), unknown/short/non-commit SHA, an
# output directory inside the work tree, a tree that contains artifacts/, and an archive
# whose extracted content does not reproduce the commit's tree hash (e.g. export-ignore /
# export-subst / eol attributes) -- the VM-side verification recomputes exactly that hash.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/integration/m0-lib.sh
source "$HERE/m0-lib.sh"

die() { echo "make-source-archive refused: $*" >&2; exit 1; }

[[ $# -eq 2 ]] || { echo "usage: $0 <full-40-hex-sha> <output-dir>" >&2; exit 2; }
SHA=$1
OUT=$2

for tool in git jq tar gzip sha256sum mktemp; do
  command -v "$tool" >/dev/null 2>&1 || die "required tool '$tool' is not installed"
done

TOP="$(git -C "$HERE" rev-parse --show-toplevel 2>/dev/null)" || die "not inside a git work tree"
m0_check_sha "$SHA" || exit 1
[[ "$(git -C "$TOP" cat-file -t "$SHA" 2>/dev/null)" == commit ]] || die "$SHA is not a commit in this repository"
m0_check_clean_tree "$TOP" || die "the work tree is not clean (commit or stash everything, including untracked files)"
git -C "$TOP" cat-file -e "$SHA:artifacts" 2>/dev/null && die "the commit tracks a top-level artifacts/ path (reserved for run evidence)"

mkdir -p "$OUT"
OUT="$(cd "$OUT" && pwd)"
case "$OUT/" in "$TOP"/*) die "the output directory must be outside the work tree" ;; esac
NAME="dso-${SHA:0:12}"
TARBALL="$NAME.tar.gz"
for f in "$TARBALL" provenance.json SHA256SUMS; do
  [[ ! -e $OUT/$f ]] || die "$OUT/$f already exists (refusing to overwrite)"
done

TREE="$(git -C "$TOP" rev-parse "$SHA^{tree}")"
BRANCH="$(git -C "$TOP" rev-parse --abbrev-ref HEAD)"
COMMIT_TIME="$(git -C "$TOP" show -s --format=%cI "$SHA")"

W="$(mktemp -d)"
trap 'rm -rf "$W"' EXIT

# Deterministic: git archive stamps every entry with the commit time; the tar umask is pinned;
# gzip -n stores no name/time. Tree only: no .git, no history.
git -C "$TOP" -c tar.umask=0022 archive --format=tar --prefix="$NAME/" "$SHA" | gzip -n -9 >"$W/$TARBALL"

# Round trip: the extracted files must hash to exactly the commit's tree.
mkdir "$W/x"
tar -xzf "$W/$TARBALL" -C "$W/x" --no-same-owner
[[ -d $W/x/$NAME && ! -e $W/x/$NAME/.git ]] || die "unexpected archive layout"
BACK="$(m0_tree_hash "$W/x/$NAME")" || die "could not hash the extracted archive"
[[ $BACK == "$TREE" ]] || die "the archive does not reproduce the commit tree ($BACK != $TREE); check .gitattributes export-ignore/export-subst/eol"

TSHA="$(sha256sum "$W/$TARBALL" | cut -d' ' -f1)"
ver() { "$@" 2>&1 | head -n 1; }
jq -n --arg sha "$SHA" --arg tree "$TREE" --arg branch "$BRANCH" --arg ctime "$COMMIT_TIME" \
  --arg created "$(m0_now)" --arg tarball "$TARBALL" --arg tsha "$TSHA" --arg prefix "$NAME/" \
  --arg git "$(ver git --version)" --arg gz "$(ver gzip --version)" --arg tar "$(ver tar --version)" \
  --arg sums "$(ver sha256sum --version)" --arg jqv "$(ver jq --version)" --arg bashv "$BASH_VERSION" '
  {schema: 1, sha: $sha, tree_sha: $tree, branch: $branch, commit_time: $ctime, created: $created,
   archive: {file: $tarball, format: "tar.gz (git archive | gzip -n -9)", prefix: $prefix, sha256: $tsha, contains_history: false},
   tools: {git: $git, gzip: $gz, tar: $tar, sha256sum: $sums, jq: $jqv, bash: $bashv},
   verification: "extract, remove nothing, then run-all.sh recomputes the git tree hash of the files and requires it to equal tree_sha; pin sha out-of-band with DSO_IT_EXPECT_SHA"}' >"$W/provenance.json"

cp -- "$W/$TARBALL" "$OUT/$TARBALL"
cp -- "$W/provenance.json" "$OUT/provenance.json"
(cd "$OUT" && sha256sum "$TARBALL" provenance.json >SHA256SUMS)

echo "source archive for $SHA (tree $TREE, branch $BRANCH)"
echo "  $OUT/$TARBALL"
echo "  $OUT/provenance.json"
echo "  $OUT/SHA256SUMS"
cat "$OUT/SHA256SUMS"
echo "Pin out-of-band on the VM:  DSO_IT_EXPECT_SHA=$SHA"
