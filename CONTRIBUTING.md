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

## Adding a New Provider

DSO is designed to be extensible. To add a new secret provider (e.g., GCP Secret Manager), follow these steps:

### 1. Implement the Interface
Your provider must implement the `SecretProvider` interface defined in `pkg/api/plugin.go`. Here is a minimal skeleton:

```go
package main

import "github.com/docker-secret-operator/dso/pkg/api"

type MyProvider struct {
    client interface{} // Your SDK client
}

func (p *MyProvider) Init(config map[string]string) error {
    // 1. Parse your config (e.g., region, project_id)
    // 2. Initialize your SDK client
    return nil
}

func (p *MyProvider) GetSecret(name string) (map[string]string, error) {
    // 1. Call your secret manager API
    // 2. Return a map of keys and values
    return map[string]string{"KEY": "VALUE"}, nil
}
```

### 2. Create the Plugin Entrypoint
Create a new directory in `cmd/plugins/dso-provider-<name>`. 
Use the `plugin.Serve` helper to wrap your implementation into an RPC-modeled plugin. Reference `cmd/plugins/dso-provider-vault/main.go` for the boilerplate.

### 3. Register the Type
Add your provider string identifier to the factory in `internal/providers/store.go`. This allows the `dso.yaml` to recognize your `type: <name>` field.

### 4. Verification
- Add unit tests for your `GetSecret` logic.
- Verify with a sample config:
  ```yaml
  providers:
    my-new-cloud:
      type: <name>
      config: { ... }
  ```
## Development Workflow

To set up a local development environment for DSO:

> [!NOTE] 
> DSO is primarily intended to be run as a Docker CLI plugin (`docker-dso`). For local development and testing, you should build and run the `docker-dso` binary directly.

### 1. Quick Development Loop
To rapidly iterate on configuration schema or CLI features:
```bash
go build -o docker-dso ./cmd/docker-dso
./docker-dso validate -c examples/dso-minimal.yaml
```
This loop ensures your core logic and schema mappings are correct without the overhead of the full plugin installation.

### 2. Run Locally
You can run the Agent directly from the source for testing:
```bash
go run cmd/docker-dso/main.go agent --config examples/dso-minimal.yaml
```

### 3. Test Configuration Changes
Before deploying your changes, use the validation logic to sanity check your schema:
```bash
go run cmd/docker-dso/main.go validate -c your-test-config.yaml
```

### 3. Add a Secret Provider
If you're implementing a new backend:
- **Interface**: Define the logic in `pkg/provider/`.
- **Plugin binary**: Create a entrypoint in `cmd/plugins/dso-provider-<name>/`.
- **Factory**: Register your new type in `internal/providers/store.go`.

---

Thank you for helping make DSO better.
