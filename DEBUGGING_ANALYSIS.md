# DSO Setup Wizard Issues - Root Cause Analysis

## Problem Summary
When running `sudo docker dso setup` in a piped context (e.g., `curl | sudo bash`), the setup completes but then tries to run `docker dso bootstrap agent` which fails with an interactive prompt error when asking for AWS region.

## Error Output
```
[ERROR] Failed to prompt for AWS region [error [prompts] INTERACTIVE_PROMPT_FAILED: Interactive prompt failed. Run with --non-interactive for automated setup. (EOF)]
[ERROR] Bootstrap failed [error [bootstrap] CONFIG_VALIDATION: Configuration validation failed: failed to configure provider: aws]
```

## Root Cause Analysis

### Issue 1: Missing CLI Flags in Bootstrap Command
**Location**: `internal/cli/bootstrap.go`

The setup wizard tries to call:
```go
bootstrapCmd := exec.Command("sudo", "docker", "dso", "bootstrap", "agent", "--provider", detectedProvider.Provider, "--non-interactive")
```

But the `NewBootstrapCmd()` function in `bootstrap.go` only accepts:
- `--enable-nonroot` flag (line 76)

It does NOT accept:
- `--provider` flag ❌
- `--non-interactive` flag ❌

So these flags are silently ignored by Cobra, and they never reach the bootstrap implementation.

### Issue 2: Missing AWS Region Handling in Non-Interactive Mode
**Location**: `internal/bootstrap/agent.go` lines 308-326

When AWS provider is used without an explicit region in non-interactive mode:
1. Line 308: Checks `opts.AWSRegion` (empty because flag not parsed)
2. Lines 310-313: Checks environment variables (likely empty in setup context)
3. Lines 315-322: If NOT non-interactive, **prompts user** for region
4. Problem: In piped setup, stdin is EOF, so prompt fails

The code at line 324-326 has a fallback to "us-east-1", but it only reaches that if the prompt succeeds or is skipped.

## Failures Explained

1. **setup.go** passes `--non-interactive` flag to bootstrap command
2. **bootstrap.go** doesn't parse this flag (no flag definition)
3. **agent.go** receives `opts.NonInteractive = false` (default)
4. **agent.go** tries to prompt for AWS region (line 318)
5. **Interactive prompter** tries to read from stdin
6. **EOF error** because stdin is closed in piped context
7. **Bootstrap fails** before reaching the fallback default

## Impact
- Users cannot use `curl | sudo bash` installation method for automated setup
- Setup wizard appears to fail after completing all steps
- Non-root users cannot bootstrap in automated contexts
- Any CI/CD automation attempting setup will fail

## Solution Required

### Fix 1: Add Missing Flags to Bootstrap Command
Add CLI flags to parse `--provider` and `--non-interactive`:
```go
cmd.Flags().StringVar(&provider, "provider", "", "Cloud provider: aws, azure, vault, huawei")
cmd.Flags().BoolVar(&nonInteractive, "non-interactive", false, "Non-interactive mode (skip prompts)")
```

Pass these to bootstrap options:
```go
opts := &bootstrap.BootstrapOptions{
    Provider: provider,
    NonInteractive: nonInteractive,
    // ... other options
}
```

### Fix 2: Improve AWS Region Handling
Ensure fallback to default region works in non-interactive mode:
- Current code already has fallback at line 324-326
- Ensure it's reached when prompt is skipped (it should be)
- Add explicit log message when using fallback

### Fix 3: Set Sensible Defaults for Other Providers
Similar handling for Azure, Huawei, Vault:
- Use environment variables as primary source
- Use CLI options as override
- Skip prompts in non-interactive mode
- Use provider-specific defaults

## Files Requiring Changes

1. **internal/cli/bootstrap.go**
   - Add `--provider` flag to bootstrap command (line ~47)
   - Add `--non-interactive` flag to bootstrap command
   - Update `bootstrapAgent()` to read and pass these flags
   - Update `bootstrapLocal()` similarly for consistency

2. **internal/bootstrap/agent.go** (likely no changes needed, logic is already there)
   - Verify fallback behavior is reached when nonInteractive=true
   - Add clearer logging

3. **internal/cli/setup.go** (no changes, already passing flags correctly)
   - Verified: already calling with --provider and --non-interactive

## Testing Checklist

After fixes:
- [ ] `docker dso setup` works in interactive mode
- [ ] `docker dso setup --auto-detect` works with auto-detected AWS region
- [ ] `curl | sudo bash` install → setup → bootstrap completes without errors
- [ ] Fallback region "us-east-1" is used when not provided
- [ ] All four providers (AWS, Azure, Vault, Huawei) handled correctly
- [ ] Non-interactive flag prevents any user prompts
- [ ] Systemd integration completes successfully
