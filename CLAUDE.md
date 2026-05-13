# CLAUDE.md

# Docker Secret Operator (DSO) Development Guidelines

Behavioral and engineering guidelines for contributing to DSO.

These rules exist to reduce common LLM coding mistakes, prevent overengineering, preserve operational correctness, and maintain production reliability.

DSO is an infrastructure/runtime system dealing with:

* secret lifecycle management
* container orchestration
* runtime rotation
* event-driven reconciliation
* failure recovery
* concurrency safety
* operational correctness

Because of this, stability and predictability matter more than speed or cleverness.

---

# 1. Think Before Coding

## Do not assume runtime behavior

Before implementing anything:

* Explicitly state assumptions.
* If something is ambiguous, ask instead of guessing.
* Surface tradeoffs clearly.
* Do not silently choose architecture changes.
* Prefer understanding existing runtime behavior before modifying it.

For DSO specifically:

* Understand rotation flow before changing it.
* Understand reconciliation behavior before modifying event handling.
* Understand container lifecycle implications before changing Docker operations.
* Understand concurrency implications before changing locks, queues, caches, or state handling.

If uncertain:

* stop
* explain uncertainty
* ask questions

Never invent behavior that does not already exist in the system.

---

# 2. Simplicity First

## Minimum code. Maximum correctness.

DSO is infrastructure software.

Infrastructure systems become dangerous when:

* abstractions grow unnecessarily
* recovery logic becomes unpredictable
* state handling becomes complicated
* hidden behavior accumulates

Always prefer:

* simple flows
* deterministic behavior
* explicit logic
* readable recovery paths

Do NOT add:

* speculative abstractions
* future-proofing layers
* generic frameworks
* unnecessary interfaces
* configuration options that were not requested
* hidden magic behavior

Ask yourself:

* Would a production SRE understand this quickly?
* Would this be debuggable during an outage?
* Is this introducing operational risk?

If the solution feels overly clever:
rewrite it simpler.

---

# 3. Surgical Changes Only

## Touch only what is required

When modifying DSO:

* Do NOT refactor unrelated code.
* Do NOT reformat unrelated files.
* Do NOT rename things unnecessarily.
* Do NOT rewrite stable systems without reason.
* Do NOT introduce architectural churn.

Every changed line should directly map to:

* the bug
* the feature
* the operational fix
* the requested improvement

Preserve:

* existing APIs
* runtime behavior
* operational expectations
* deployment compatibility

---

# 4. Runtime Correctness Over Code Elegance

DSO is a runtime orchestration system.

Correct behavior during failure matters more than:

* clean abstractions
* pretty architecture
* ideal patterns

Always prioritize:

* deterministic recovery
* rollback safety
* crash consistency
* reconciliation correctness
* state integrity
* operational visibility

Never optimize for elegance at the cost of operational safety.

---

# 5. Failure-State Thinking

Always think about:

* Docker daemon restarts
* network partitions
* provider timeouts
* event floods
* SIGTERM/SIGKILL during operations
* stale locks
* partial writes
* orphaned containers
* duplicate events
* concurrent rotations
* queue saturation
* plugin crashes
* disk pressure
* memory pressure

DSO must behave safely during failure, not just during success.

Any new logic must answer:

* What happens if this crashes halfway?
* What happens during restart?
* What happens during timeout?
* What happens if multiple operations race?

---

# 6. Goal-Driven Execution

Transform tasks into verifiable goals.

Example:

```text
1. Reproduce issue
   → verify: failing test or reproducible scenario exists

2. Implement minimal fix
   → verify: bug no longer reproducible

3. Validate runtime safety
   → verify: no races, no leaks, rollback still works

4. Run regression validation
   → verify: existing tests still pass
```

Avoid vague implementation goals like:

* "improve architecture"
* "optimize system"
* "make it scalable"

Use measurable goals instead.

---

# 7. Testing Requirements

For all runtime or operational changes:

Run:

```bash
go test ./...
go test -race ./...
```

When applicable:

* add regression tests
* add concurrency tests
* add integration tests
* add recovery tests
* add chaos/failure tests

Validate:

* no goroutine leaks
* no FD leaks
* no orphan containers
* no stale locks
* no silent failures
* no memory growth regressions

Do NOT mark work complete without verification.

---

# 8. Concurrency & State Safety

DSO contains:

* queues
* caches
* locks
* provider workers
* reconciliation loops
* runtime state transitions

Treat all shared state as dangerous.

Before modifying concurrency-related logic:

* identify ownership
* identify lifecycle
* identify cancellation behavior
* identify cleanup guarantees

Avoid:

* unsafe shared mutable state
* lock ordering ambiguity
* hidden goroutines
* blocking event loops
* unbounded retries
* silent retry storms

Prefer:

* explicit ownership
* bounded work
* deterministic cleanup
* context-aware cancellation

---

# 9. Operational Visibility

DSO must be operable during incidents.

When adding runtime behavior:

* ensure failures are observable
* ensure degraded states are visible
* ensure logs are actionable
* ensure operators can debug problems

Avoid:

* silent failures
* swallowed errors
* hidden retries
* vague logs

Prefer:

* structured logging
* explicit warnings
* recovery visibility
* operational context in errors

---

# 10. Documentation Rules

## DO NOT generate unnecessary markdown or docs

Unless explicitly requested:

* DO NOT create new `.md` files
* DO NOT generate audit reports
* DO NOT generate roadmap docs
* DO NOT generate architecture docs
* DO NOT generate implementation summaries
* DO NOT generate temporary planning docs

Only modify documentation when:

* directly requested
* required for user-facing functionality
* required for operational clarity
* required for contributor onboarding

When documentation IS required:

* keep it concise
* keep it technically accurate
* avoid marketing language
* avoid exaggerated production claims

README changes should focus on:

* project understanding
* architecture clarity
* installation
* usage
* operational concepts

NOT internal debugging history.

---

# 11. Avoid Overengineering

DSO is evolving into a production-grade infrastructure system.

Do NOT prematurely add:

* distributed consensus
* orchestration frameworks
* HA clustering
* plugin marketplaces
* advanced schedulers
* generalized orchestration layers

Implement only what is needed now.

Prefer:

* stable incremental evolution
* understandable systems
* operational simplicity

---

# 12. Production Mindset

Assume:

* operators make mistakes
* networks fail
* containers crash
* providers timeout
* Docker behaves unexpectedly
* events arrive out of order
* partial state exists

Design for:

* reconciliation
* recovery
* rollback
* observability
* operational safety

The system should:

* fail predictably
* recover deterministically
* avoid hidden corruption
* minimize operator surprise

---

# Success Criteria

These guidelines are working if:

* diffs stay focused and small
* runtime behavior becomes more deterministic
* operational bugs decrease
* recovery behavior improves
* unnecessary abstractions disappear
* fewer rewrites are needed
* contributors understand the system faster
* production debugging becomes easier
