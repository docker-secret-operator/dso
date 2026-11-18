#!/usr/bin/env bash
# summarize-results.sh <test.json> -- print one line per test from a `go test -json`
# stream: its final outcome, or INTERRUPTED if it started and never finished (for
# example cut off by the go test timeout). Printed to stdout so it lands in the CI job
# log, which is kept whatever the outcome; the artifact upload only runs on failure and
# a timeout must not erase which cases ran. Read-only; never fails the caller.
set -u
json="${1:-}"
[[ -r "$json" ]] || { echo "summarize-results: cannot read '${json}'"; exit 0; }

tmp="$(mktemp)"; trap 'rm -f "$tmp" "$tmp.run" "$tmp.fin"' EXIT
grep -oE '"Action":"(pass|fail|skip)","Package":"[^"]*","Test":"[^"]*"' "$json" \
  | sed -E 's/"Action":"([a-z]+)","Package":"[^"]*","Test":"([^"]*)"/\1\t\2/' >"$tmp.fin" || true
grep -oE '"Action":"run","Package":"[^"]*","Test":"[^"]*"' "$json" \
  | sed -E 's/.*"Test":"([^"]*)"/\1/' | sort -u >"$tmp.run" || true
cut -f2 "$tmp.fin" | sort -u >"$tmp"

echo "---- per-test results ($(basename "$json")) ----"
sort -t$'\t' -k2 "$tmp.fin" | awk -F'\t' '{printf "%-12s %s\n", toupper($1), $2}'
# A test with subtests is "running" until the last subtest ends; list only leaves and
# parents that never finished, which is exactly what a timeout leaves behind.
comm -23 "$tmp.run" "$tmp" | awk '{printf "%-12s %s\n", "INTERRUPTED", $0}'
echo "---- totals: pass=$(grep -c '^pass' "$tmp.fin") fail=$(grep -c '^fail' "$tmp.fin") skip=$(grep -c '^skip' "$tmp.fin") interrupted=$(comm -23 "$tmp.run" "$tmp" | wc -l) ----"
exit 0
