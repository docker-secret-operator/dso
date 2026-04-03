# Contributing to Docker Secret Operator (DSO)

Thanks for your interest in contributing to DSO! Whether it's a bug fix, a new provider, better docs, or just a typo — every contribution matters.

This guide explains how to get started.

## Code of Conduct

By participating in this project, you agree to follow our [Code of Conduct](CODE_OF_CONDUCT.md). We want this to be a welcoming space for everyone.

## How to contribute

### 1. Fork the repository

Click the **Fork** button on GitHub and clone your fork locally:

```bash
git clone https://github.com/<your-username>/dso.git
cd dso
```

### 2. Create a branch

Create a branch with a descriptive name:

```bash
git checkout -b fix/rotation-cooldown
# or
git checkout -b feat/gcp-provider
```

Use prefixes like `fix/`, `feat/`, `docs/`, or `test/` to keep things organized.

### 3. Make your changes

- Write clean, readable Go code
- Follow [Uber Go Style Guide](https://github.com/uber-go/guide/blob/master/style.md)
- Add or update tests if your change affects behavior
- Run `go build ./...` and `go test ./...` before committing

### 4. Sign your commits (DCO)

All commits **must** include a `Signed-off-by` line. This is a lightweight way to certify that you wrote the code (or have the right to submit it) under the project's license.

Add it automatically with the `-s` flag:

```bash
git commit -s -m "fix: prevent duplicate rotation within cooldown window"
```

This adds a line like:

```
Signed-off-by: Your Name <your@email.com>
```

If you forget, you can amend your last commit:

```bash
git commit --amend -s
```

> **Why DCO?** It's a simple, developer-friendly way to track contributions without requiring a full CLA. It's used by the Linux kernel, CNCF projects, and many others.

### 5. Push and open a Pull Request

```bash
git push origin fix/rotation-cooldown
```

Then open a PR on GitHub against the `main` branch. In your PR description:

- Explain **what** you changed and **why**
- Reference any related issues (e.g., `Fixes #42`)
- Include screenshots or logs if it helps

## What makes a good PR?

- **Small and focused** — One concern per PR
- **Tested** — Add or update tests where reasonable
- **Documented** — Update docs if behavior changes
- **Signed** — Every commit has a DCO sign-off

## Reporting bugs

Open a [GitHub Issue](https://github.com/docker-secret-operator/dso/issues) with:

- A clear title describing the problem
- Steps to reproduce
- What you expected vs what happened
- Any relevant logs or screenshots

## Suggesting features

We're open to ideas. Open an issue with the `enhancement` label and describe:

- What problem it would solve
- How you'd expect it to work
- Why it would be useful to other users

## Getting help

If you're stuck or have questions, open a [Discussion](https://github.com/docker-secret-operator/dso/discussions) or reach out on an issue. We're happy to help.

---

Thank you for helping make DSO better.
