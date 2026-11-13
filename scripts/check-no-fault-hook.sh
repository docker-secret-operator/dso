#!/bin/sh
# [M0-05] Fail if any given built artifact contains the fault-injection hook.
# The hook's environment-variable names (DSO_FAULT_*) live ONLY in the dsofault-tagged
# file (internal/faultinject/faultinject_dsofault.go), so their presence in a binary
# means it was built with -tags=dsofault. POSIX sh + grep only (also runs on busybox).
# Usage: check-no-fault-hook.sh FILE...
set -eu
[ "$#" -ge 1 ] || { echo "usage: $0 FILE..." >&2; exit 2; }
bad=0
for f in "$@"; do
    if [ ! -f "$f" ]; then
        echo "error: $f does not exist; cannot inspect it" >&2
        bad=1
        continue
    fi
    # -a: treat binary as text (GNU/BSD). busybox grep has no binary detection and may
    # not accept -a, so fall back to a plain count.
    if n="$(LC_ALL=C grep -a -c 'DSO_FAULT_' "$f" 2>/dev/null)"; then :; else
        rc=$?
        if [ "$rc" -eq 1 ]; then n=0; else n="$(LC_ALL=C grep -c 'DSO_FAULT_' "$f" 2>/dev/null || true)"; n="${n:-0}"; fi
    fi
    if [ "${n:-0}" -ne 0 ]; then
        echo "error: $f contains the test-only fault hook (DSO_FAULT_ x${n}); it was built with -tags=dsofault" >&2
        bad=1
    else
        echo "ok: $f has no fault hook"
    fi
done
exit "$bad"
