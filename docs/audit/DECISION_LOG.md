# Decision Log

**Purpose**: Record significant decisions made during audit implementation.  
Future maintainers will understand "why this design" without having to reverse-engineer intent.

---

## Decision Record Template

```markdown
## Decision: [Brief Title]

**Date**: YYYY-MM-DD  
**Issue**: [BUG-1, SEC-2, PRODUCT-1, etc.]  
**Decision Maker**: [Name]  
**Status**: [Proposed / Approved / Implemented / Superseded]

### Context
[Why was this decision needed? What were the constraints?]

### Options Considered
1. **Option A**: [Description]
   - Pros: [list]
   - Cons: [list]
   - Effort: [time estimate]

2. **Option B**: [Description]
   - Pros: [list]
   - Cons: [list]
   - Effort: [time estimate]

3. **Option C**: [Description]
   - Pros: [list]
   - Cons: [list]
   - Effort: [time estimate]

### Decision
**Chosen**: Option B

### Rationale
[Why was this the best choice? What trade-offs were accepted?]

### Consequences
[What becomes easier? What becomes harder? What now needs to be true?]

### Rollback Implications
[If this decision needs to be reversed, what's the cost?]

---
```

---

## Decisions (To Be Populated During Implementation)

### Decision 1: PRODUCT-1 — Merge or Archive feature/web-ui

**Date**: [TBD]  
**Status**: [Proposed]

### Context
The `feature/web-ui` branch contains 143 commits ahead of `main`, including substantial intelligence/governance engines (drift, policy, forecast, apply, compliance). The branch and main have completely diverged.

Options:
1. **Merge**: Rebase and integrate all governance work into main
2. **Archive**: Delete branch, keep only core CLI engine, clean orphaned code

### Decision
**Chosen**: [TBD — Requires stakeholder input]

**If Merge**:
- Rebase 143 commits onto current main
- Resolve conflicts (estimated 10-20 files)
- Extensive regression testing
- Update ROADMAP to reflect new Phase G scope
- New complexity in operations (SQLite, governance APIs)

**If Archive**:
- Delete `feature/web-ui` branch
- Remove orphaned `internal/webui/assets/`
- Update ROADMAP Phase G to reflect "read-only lightweight UI, deferred"
- Reduces maintenance burden, clarifies product roadmap
- Requires tracking any valuable learnings from governance code

---

### Decision 2: SEC-1 — Logging Redaction Strategy

**Date**: [TBD]  
**Status**: [Proposed]

### Context
Redaction engine exists but is unwired. Need to decide implementation approach:

1. **zapcore.Core wrapper** — Intercept all logs at core level
2. **zap.Hook middleware** — Process logs before output
3. **Call-site enforcement** — Require all error logging to call redaction function
4. **Custom logger wrapper** — Thin wrapper around zap that redacts automatically

### Considerations
- **Call-site enforcement** is risky (developers will forget)
- **Core wrapper** is transparent (all logs automatically redacted)
- **Hook approach** is between the two
- **Custom wrapper** requires changing all logger construction sites

### Decision
**Chosen**: [TBD — Will be decided during SEC-1 implementation]

---

### Decision 3: SEC-2 — Plugin Hash Verification Default

**Date**: [TBD]  
**Status**: [Proposed]

### Context
Hash verification is currently optional (env var gated). Need to decide default behavior:

1. **Fail Closed** (Recommended): Require manifest, reject unsigned plugins
2. **Fail Open** (Current): Manifest optional, unsigned plugins allowed

### Trade-offs
- **Fail Closed**: More secure, but blocks deployment if manifest missing
- **Fail Open**: More backward-compatible, but weaker security posture

### Recommendation
**Chosen**: Fail Closed

**Rationale**: Security by default. Users explicitly opting out of verification (if allowed) is better than silent bypass.

---

### Decision 4: SEC-3 — Plugin Directory Permissions in Container

**Date**: [TBD]  
**Status**: [Proposed]

### Context
Plugin directory is currently writable by daemon user. Options:

1. **Root-owned, 0555** — Read-only for daemon user
2. **Root-owned, 0750** — Writable only by root, readable by daemon
3. **Mount read-only** — Docker volume mount as read-only
4. **No change** — Accept current risk

### Decision
**Chosen**: Root-owned, 0555 (read-only)

**Rationale**: Simplest, no runtime mount complexity, prevents post-compromise persistence.

---

### Decision 5: BUG-1 — Ticker Synchronization Approach

**Date**: [TBD]  
**Status**: [Proposed]

### Context
Tickers map needs synchronization. Options:

1. **Add tickersMu to Agent struct** — Separate mutex for tickers
2. **Reuse existing tickerStopMu** — Use same mutex for both maps
3. **Refactor to goroutine channels** — Eliminate map entirely, use channels

### Decision
**Chosen**: [TBD — Will depend on implementation details]

---

## How to Use This Log

### When Reviewing Code
Reader asks: "Why was this designed this way?"  
→ Check DECISION_LOG.md for context and trade-offs

### When Proposing Changes
Maintainer thinks: "Should we change this?"  
→ Check DECISION_LOG.md to see what was deliberately chosen vs accidental

### During Retrospectives
Team asks: "What did we learn?"  
→ Review DECISION_LOG.md to identify patterns (e.g., "security decisions took longer than expected")

---

## Guidelines

### When to Record a Decision
- **Major architectural choice** (merge vs archive, new subsystem)
- **Security trade-off** (fail open vs fail closed)
- **API/compatibility decision** (breaking change or backward-compatible workaround)
- **Performance optimization** (trading memory for speed, etc.)
- **Deferred work** (deliberately deferring a fix with justification)

### What NOT to Record
- Trivial bug fixes ("added null check")
- Routine refactoring ("split function into two")
- Minor code cleanup

### Format
- Keep it concise (2-3 paragraphs per decision)
- Include date and decision maker (for context traceability)
- Always include "why this wasn't the alternative"
- Be explicit about trade-offs accepted

---

## After All Phases

Review this log to understand:
- What design choices shaped the implementation
- What constraints existed at decision time
- What became easier/harder as a result
- What would be different with different choices

This becomes the institutional memory of the audit-driven refactor.
