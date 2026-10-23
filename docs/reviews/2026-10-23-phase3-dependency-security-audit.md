# Phase 3 — dependency security audit and supply-chain hardening

Date: 2026-10-23 · Branch `feature/webui-phase1` · Base `37b09c9` (changes uncommitted)
Tools: govulncheck v1.6.0 (DB updated 2026-10-21), gosec (dev build), npm 11.17.0, Node 24.19.0 (CI uses Node 20.x).

## 1. Results at a glance

| | Before | After |
|---|---|---|
| govulncheck, reachable | 17 (Go stdlib 13, x/net 5\*, grpc 2, docker 2) | **2** (docker, no upstream fix) |
| Go toolchain | go1.25.13 | **go1.26.10** (`go 1.26.0`) |
| gosec issues | 23 (1 HIGH G115, 1 HIGH G101, …) | **0**; `#nosec` count 95 → 105, **all with a written reason** |
| npm audit | 34 (5 critical, 25 high, 4 moderate) | **9 (0 critical, 7 high, 2 moderate)**, all build/test-time |
| `npm audit --omit=dev` | n/a | 7 (same build-time chain; none ship in the static bundle) |
| `npm run lint` | interactive prompt | non-interactive, `--max-warnings=0`, in CI |

\*several advisories are reported against both net/http and x/net.

## 2. Go findings

| ID | Package / version | Advisory | Reachable | Exposure | Fixed in | Action |
|---|---|---|---|---|---|---|
| G-1 | Go stdlib 1.25.13 (net/http, net/textproto, crypto/tls, crypto/x509, html/template) | GO-2026-6599/6600/6603/6605/6607/6608/6609/6610/6611/6612/6613/6617/6809 | yes (REST API, webhook client, agent) | HTTP/2 DoS, request smuggling, ECH/x509 handling | go1.26.9 (x509: 1.26.10) | **go1.26.10**. 1.25 line has no fix (out of support); 1.27.3 also exists but 1.26.10 is the minimal compatible fix |
| G-2 | golang.org/x/net 0.55.0 | GO-2026-6603/6610/6611/6612/6617 | yes | HTTP/2 | 0.60.0 | upgraded to 0.60.0 (pulls x/crypto 0.57, x/sys 0.48, x/term 0.46, x/text 0.42) |
| G-3 | google.golang.org/grpc 1.82.1 | GO-2026-6443, GO-2026-6348 | yes | server panic / OOM | 1.82.2 / 1.83.1 — 6443 re-appeared at 1.83.1, **1.83.2** clears both | upgraded to 1.83.2 (+ genproto, otel bumps required by it) |
| G-4 | github.com/docker/docker 28.5.2 | GO-2026-4883, GO-2026-4887 | "called" only through package-init and client calls | advisories concern the Docker **daemon** AuthZ plugin path; DSO links only client packages | none | **Accepted**, listed in `.govulncheck-allowlist.txt` with rationale. Follow-up: evaluate migration to `github.com/moby/moby/client` when feature-parity allows |

`go mod tidy` + `go mod verify` clean. No `replace` directives. Dockerfile builder moved to `golang:1.26-alpine`; CI and release read `go.mod`.

### Cryptography / gosec
* **G115 `crypto/envelope.go` (`uint32(len(s))`)** — real latent defect: a field ≥ 4 GiB would silently truncate its length prefix, letting two different AAD field lists encode identically. Now `fieldLen` rejects negative / > `MaxAADFieldSize` (1 MiB, ≪ MaxUint32) before allocation; `aad`, `SecretAAD`, `DEKAAD`, `BindingAAD` return `(bytes, error)`; eight call sites in `internal/centralsvc` propagate the error. Encoding of valid input is byte-identical (pinned by `TestAAD_EncodingIsUnchangedForValidInput`), so existing ciphertext still opens. Tests cover 0, 1, max, max+1, MaxUint32(+1), negative, MinInt/MaxInt, malformed nonce/ciphertext, plaintext cap.
* **G101 (`sqlite.go`)** — false positive: a SQL SELECT constant whose identifier contained "secret". Renamed; no suppression.
* **G124 (session cookie)** — `Secure` follows `Manager.Secure` (default true; false only for plain-HTTP loopback dev); HttpOnly + SameSite=Strict always set. Justified `#nosec`.
* **G304 ×6** — all paths are operator-configured or pass through `securefs` (O_NOFOLLOW, directory-chain and owner/mode checks). Justified `#nosec` with the reason inline. **G302** — directory mode in a test helper (execute bit needed). **G104 ×12** — close/remove errors now handled (`f.Close()` result checked on the backup file; best-effort cleanups made explicit with `_ =`).
* Remaining 11 `#nosec` without prose reasons (exec of fixed binaries, plugin exec, `0644` manifest) got reasons. The other ~90 pre-existing suppressions already carried a reason; they were **not individually re-validated** in this phase.

### REST API bind failure (agent availability)
Decision: the API is **mandatory** (WebUI, `dso logs --api`, health probes depend on it). `StartRESTServerSupervised` binds with `net.Listen` before returning (address-in-use / invalid address return an error), serves in a goroutine, reports unexpected termination once on a channel, and has an idempotent shutdown that also fires on context cancel with no leaked goroutine. The agent aborts and unwinds on a bind error, and shuts down gracefully with exit 1 if the listener later dies; SIGINT/SIGTERM is not a failure. `StartRESTServer` keeps its old signature. Tests (race-clean): success, in-use, three invalid addresses, injected Accept failure, listener closed underneath, context-cancel, pre-check failure binds nothing, plus an agent-level abort test.

## 3. Web / Node findings

Deployment model: Next.js `output: 'export'`; the Go binary serves the static files. **No Node runtime in production**, so Next.js server-side advisories (Image Optimizer, Server Actions, RSC, rewrites, middleware, WebSocket upgrade) have no runtime surface in a DSO deployment. They did apply to `next dev` and to anyone running `next start`, and the version was flagged critical, so it was upgraded rather than waved off.

| ID | Package | Advisory class | Actual exposure | Action |
|---|---|---|---|---|
| N-1 | next 14.2.35 (24 advisories, 3 critical RCE) | server-only features | none in static export; dev server only | → **15.5.28** (all 14.x fail; 15.5.28 is the lowest release clearing every listed range; 16 not required). Build, 130 tests, static export succeed |
| N-2 | axios 1.17.0 (23 advisories) | mostly Node adapters, proxy, form serialisation; some fetch/xhr prototype-pollution gadgets | **runs in the browser** (`lib/api-client.ts`, `lib/api-fetch.ts`); gadgets need a separate prototype-pollution bug to matter | → **1.20.0** (same major) |
| N-3 | postcss ≤ 8.5.22, plus next's bundled 8.4.31 | sourceMappingURL file read / XSS in stringify | build-time over our own CSS only | direct → 8.5.31; `overrides: { next: { postcss: "$postcss" } }` forces next's nested copy (stale lock entry removed so it applies). Verified `npm ls postcss`: single 8.5.31 |
| N-4 | vitest/vite/@vitest/ui/coverage/tinypool/esbuild (criticals) | UI-server file read, worker prototype pollution, dev-server request exposure | dev/test only; `test:ui` not run in CI | vitest family → **4.1.12**, vite → **7.3.7** (vitest 5 / vite 8 need Node ≥ 22.12, CI is Node 20). 130/130 tests pass |
| N-5 | @typescript-eslint 7 (via eslint-config-next), eslint 8 | glob/fast-glob chain | lint-time | **eslint 9.39.5** + typescript-eslint 8.72.0. ESLint 9 is now maintenance-only; eslint-config-next 15 does not accept ESLint 10 — revisit with Next 16 |
| N-6 | brace-expansion, browserslist, follow-redirects, js-yaml, nanoid, undici, form-data, baseline-browser-mapping, source-map-js, postcss-selector-parser (partly) | assorted DoS | transitive, build-time | cleared by `npm audit fix` (lockfile only) |

**Remaining (9; 0 critical):** `braces` (no patched release exists), `micromatch`, `fast-glob`, `chokidar`, `tailwindcss` 3.x chain, `@next/eslint-plugin-next`/`eslint-config-next` (via fast-glob), `postcss-selector-parser`, `postcss-nested`. All are file-glob / selector parsing used at lint or CSS-build time against our own sources; none is in the shipped bundle. The only fix offered is Tailwind 4 (major, separate migration) — recorded as follow-up. CI now blocks on `--audit-level=critical` as a ratchet.

### Dependencies removed (verified zero references in source, tests, configs, scripts, CI)
`@tanstack/react-query`, `@tanstack/react-table`, `@tanstack/react-virtual`, `@monaco-editor/react`, `monaco-editor`, `@radix-ui/react-dialog` (still installed transitively through `cmdk`, which is used; `@radix-ui/react-slot` is used by `button.tsx` and kept). Bundle effect: none (unused code was never imported); six fewer direct dependencies.

### Lint gate
`eslint.config.mjs` (flat config; `next/core-web-vitals` + `next/typescript`), `npm run lint = eslint . --max-warnings=0`, added to the CI `webui` job. Baseline had 4 errors / 15 warnings: all fixed in code (typed errors instead of `any`, unused imports, memoised derived arrays, misplaced `eslint-disable` comments moved to the dependency-array line). One scoped exception: `no-require-imports` off for `tailwind.config.js` / `postcss.config.js` (CommonJS configs). `tests/e2e/**` is excluded until the E2E rewrite.

### Embedded assets
`internal/webui/assets` regenerated with `scripts/build-webui.sh` (the CI diff gate). Two consecutive builds produce identical output (checksummed). Auth (HttpOnly cookie via Go), CSRF/Origin handling and security headers are server-side in Go and were not modified.

## 4. CI / supply chain
Done: lint step, critical-level npm audit, stale govulncheck allowlist entry (GO-2026-5668) removed and rationale rewritten. Existing: gosec pinned at v2.25.0, blocking govulncheck with allowlist.
Recommended separately: pin `govulncheck@latest` to a version; pin GitHub Actions by SHA; Dependabot/Renovate for `gomod`, `npm`, `github-actions`; `npm ci --ignore-scripts` plus an explicit allow-list (npm 11 now warns that `esbuild`/`unrs-resolver` install scripts are not approved); SBOM (CycloneDX) and cosign/SLSA provenance on release artifacts via goreleaser; run `npm audit signatures`; Tailwind 4 and Next 16/ESLint 10 migration; Docker client migration.

## 5. Verification (this tree)
`go vet ./...`, `gofmt -l`, `git diff --check` clean; `go test ./...` and `go test -race ./...` pass; `govulncheck ./...` → 2 (docker, allow-listed); `gosec ./...` → 0 issues; web: `tsc --noEmit`, `npm run lint`, `npm test` (130/130), `npm run build`, `npm audit --audit-level=critical` all succeed.
