# Deferred production qualification and release blockers

The Docker-free checkpoints (M0, M1, M2) establish source-level and fake-daemon behavior only. The following have **not** been demonstrated and remain open:

- N19 managed file delivery on a real Docker engine (the gated startup, tmpfs mount and exec-injection premise are runtime-unverified). **Open production release blocker.**
- Real Docker functional tests and image-build tests; compiled integration scenarios are not counted as executed.
- Fault-injection crash-harness execution against a real host.
- Real traffic and availability measurements; no zero-downtime claim for restart rotations or agent crashes.
- Live systemd behavior and hosted CI.
- Factual verification of the N7 and N8 findings and acceptance of the known Docker advisories.
- Real keyless signing and production release approval.

Unsupported configurations stay explicit (for example images without a shell for gated delivery). Moving to the next milestone does not close any item above.
