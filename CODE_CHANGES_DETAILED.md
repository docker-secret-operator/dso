# Detailed Code Changes - DSO Setup Wizard Fix

## File 1: `internal/cli/bootstrap.go`

### Change 1: Add Flag Variables (Lines 46-56)

**Before**: Only had `enableNonRootAccess`
```go
var enableNonRootAccess bool
```

**After**: Added full set of bootstrap command flags
```go
// Bootstrap command flags
var (
	enableNonRootAccess bool
	bootstrapProvider   string
	bootstrapNonInteractive bool
	bootstrapAWSRegion  string
	bootstrapAzureVaultURL string
	bootstrapHuaweiRegion string
	bootstrapHuaweiProjectID string
	bootstrapVaultAddress string
)
```

### Change 2: Add Flag Definitions in NewBootstrapCmd() (Lines 92-99)

**Before**: No provider or non-interactive flags
```go
cmd.Flags().BoolVar(&enableNonRootAccess, "enable-nonroot", false,
	"Automatically configure current user for non-root DSO access (agent mode only)")
```

**After**: Added comprehensive flags for automated setup
```go
// Add flags for automated/non-interactive setup
cmd.Flags().StringVar(&bootstrapProvider, "provider", "", "Cloud provider: aws, azure, vault, huawei (skips provider selection)")
cmd.Flags().BoolVar(&bootstrapNonInteractive, "non-interactive", false, "Non-interactive mode (skip all prompts, use defaults)")
cmd.Flags().StringVar(&bootstrapAWSRegion, "aws-region", "", "AWS region for automated setup (default: us-east-1)")
cmd.Flags().StringVar(&bootstrapAzureVaultURL, "azure-vault-url", "", "Azure Key Vault URL for automated setup")
cmd.Flags().StringVar(&bootstrapHuaweiRegion, "huawei-region", "", "Huawei region for automated setup")
cmd.Flags().StringVar(&bootstrapHuaweiProjectID, "huawei-project-id", "", "Huawei Project ID for automated setup")
cmd.Flags().StringVar(&bootstrapVaultAddress, "vault-address", "", "HashiCorp Vault address for automated setup")
```

### Change 3: Update Help Text in NewBootstrapCmd() (Line 72)

**Before**: 
```
Examples:
  docker dso bootstrap local                           # For local development
  sudo docker dso bootstrap agent                      # For production deployment
  sudo docker dso bootstrap agent --enable-nonroot     # Production + non-root CLI access
```

**After**: Added automated setup example
```
Examples:
  docker dso bootstrap local                           # For local development
  sudo docker dso bootstrap agent                      # For production deployment
  sudo docker dso bootstrap agent --enable-nonroot     # Production + non-root CLI access
  sudo docker dso bootstrap agent --provider aws --non-interactive  # Automated setup
```

### Change 4: Update bootstrapLocal() Function (Lines 101-112)

**Before**: 
```go
opts := &bootstrap.BootstrapOptions{
	Mode:           bootstrap.ModeLocal,
	Provider:       "", // Will prompt user
	NonInteractive: false,
	Force:          false,
	DryRun:         false,
	Timeout:        30 * 60,
	Context:        ctx,
}
```

**After**: Pass flag values
```go
opts := &bootstrap.BootstrapOptions{
	Mode:           bootstrap.ModeLocal,
	Provider:       bootstrapProvider, // From --provider flag, or "" to prompt user
	NonInteractive: bootstrapNonInteractive,
	Force:          false,
	DryRun:         false,
	Timeout:        30 * 60,
	Context:        ctx,
}
```

### Change 5: Update bootstrapAgent() Function (Lines 151-172)

**Before**:
```go
opts := &bootstrap.BootstrapOptions{
	Mode:                  bootstrap.ModeAgent,
	Provider:              "", // Will prompt user or detect
	NonInteractive:        false,
	Force:                 false,
	DryRun:                false,
	EnableNonRootAccess:   enableNonRootAccess,
	Timeout:               30 * 60,
	Context:               ctx,
}
```

**After**: Added provider-specific configuration options
```go
opts := &bootstrap.BootstrapOptions{
	Mode:                  bootstrap.ModeAgent,
	Provider:              bootstrapProvider, // From --provider flag, or "" to prompt/detect
	NonInteractive:        bootstrapNonInteractive,
	Force:                 false,
	DryRun:                false,
	EnableNonRootAccess:   enableNonRootAccess,
	Timeout:               30 * 60,
	Context:               ctx,
	// Cloud-specific configuration options
	AWSRegion:            bootstrapAWSRegion,
	AzureVaultURL:        bootstrapAzureVaultURL,
	HuaweiRegion:         bootstrapHuaweiRegion,
	HuaweiProjectID:      bootstrapHuaweiProjectID,
	VaultAddress:         bootstrapVaultAddress,
}
```

---

## File 2: `internal/cli/setup.go`

### Change: Enhanced Bootstrap Invocation (Lines 187-226)

**Before**: Simple command execution
```go
if deploymentMode == "agent" {
	fmt.Println("🚀 Starting DSO agent...")
	
	configData, _ := os.ReadFile(configPath)
	
	bootstrapCmd := exec.Command("sudo", "docker", "dso", "bootstrap", "agent", "--provider", detectedProvider.Provider, "--non-interactive")
	bootstrapCmd.Stdout = os.Stdout
	bootstrapCmd.Stderr = os.Stderr
	if err := bootstrapCmd.Run(); err != nil {
		fmt.Printf("⚠ Agent startup may have encountered issues: %v\n", err)
		fmt.Println("  Check status with: sudo docker dso system status")
	}
	
	if len(configData) > 0 {
		os.WriteFile(configPath, configData, 0644)
	}
}
```

**After**: Enhanced with metadata extraction and provider-specific flags
```go
if deploymentMode == "agent" {
	fmt.Println("🚀 Starting DSO agent...")

	configData, _ := os.ReadFile(configPath)

	// Build bootstrap command with --non-interactive and provider to avoid interactive prompts (like AWS region) hitting EOF
	bootstrapArgs := []string{"sudo", "docker", "dso", "bootstrap", "agent", "--provider", detectedProvider.Provider, "--non-interactive"}

	// Add provider-specific parameters if available from cloud detection
	if detectedProvider.Metadata != nil {
		if region, ok := detectedProvider.Metadata["region"]; ok && region != "" {
			switch detectedProvider.Provider {
			case "aws":
				bootstrapArgs = append(bootstrapArgs, "--aws-region", region)
			case "huawei":
				bootstrapArgs = append(bootstrapArgs, "--huawei-region", region)
			}
		}
		if projectID, ok := detectedProvider.Metadata["project_id"]; ok && projectID != "" {
			bootstrapArgs = append(bootstrapArgs, "--huawei-project-id", projectID)
		}
		if vaultURL, ok := detectedProvider.Metadata["vault_url"]; ok && vaultURL != "" {
			bootstrapArgs = append(bootstrapArgs, "--azure-vault-url", vaultURL)
		}
		if vaultAddr, ok := detectedProvider.Metadata["vault_address"]; ok && vaultAddr != "" {
			bootstrapArgs = append(bootstrapArgs, "--vault-address", vaultAddr)
		}
	}

	bootstrapCmd := exec.Command(bootstrapArgs[0], bootstrapArgs[1:]...)
	bootstrapCmd.Stdout = os.Stdout
	bootstrapCmd.Stderr = os.Stderr
	if err := bootstrapCmd.Run(); err != nil {
		fmt.Printf("⚠ Agent startup may have encountered issues: %v\n", err)
		fmt.Println("  Check status with: sudo docker dso system status")
	}

	if len(configData) > 0 {
		os.WriteFile(configPath, configData, 0644)
	}
}
```

---

## Key Differences

### Before vs After Command Line Behavior

**BEFORE (Broken)**:
```bash
$ sudo docker dso setup
# Setup wizard tries to run:
$ sudo docker dso bootstrap agent --provider aws --non-interactive
# ❌ Flags ignored because bootstrap command didn't recognize them
# ❌ Bootstrap defaults to Interactive mode, tries to prompt for AWS region
# ❌ Gets EOF from stdin, fails
```

**AFTER (Fixed)**:
```bash
$ sudo docker dso setup
# Setup wizard runs:
$ sudo docker dso bootstrap agent --provider aws --non-interactive
# ✅ bootstrap command parses --provider flag → opts.Provider = "aws"
# ✅ bootstrap command parses --non-interactive flag → opts.NonInteractive = true
# ✅ Bootstrap skips prompts, uses defaults or detected metadata
# ✅ Succeeds without user interaction
```

---

## No Changes Required In

### `internal/bootstrap/agent.go`
- Already has logic to:
  - Check opts.AWSRegion for explicit region
  - Check environment variables if flag not provided
  - Skip prompts if opts.NonInteractive == true
  - Use "us-east-1" as fallback default
- This code just needs the options to reach it (which they now do!)

### `internal/bootstrap/types.go`
- Already has all necessary fields in BootstrapOptions struct
- No changes needed

### `internal/bootstrap/prompts.go`
- Already has error handling for EOF
- No changes needed

---

## Testing the Changes

### Test 1: Verify Flags Are Recognized
```bash
docker dso bootstrap agent --help | grep -E "provider|non-interactive|aws-region"
# Should show all new flags with descriptions
```

### Test 2: Verify Automated Setup Works
```bash
# On AWS EC2:
sudo docker dso setup  # Auto-detects AWS
# Should complete without prompts

# Or explicitly:
sudo docker dso bootstrap agent --provider aws --aws-region us-west-2 --non-interactive
# Should use us-west-2
```

### Test 3: Verify Interactive Mode Still Works
```bash
sudo docker dso bootstrap agent
# Should prompt for provider and region (backward compatibility)
```

### Test 4: Verify Piped Installation Works
```bash
curl -fsSL https://raw.githubusercontent.com/docker-secret-operator/dso/main/scripts/install.sh | sudo bash
sudo docker dso setup
# Should complete successfully (this was the original failure)
```

---

## Summary

**Total Changes**:
- 2 files modified
- 1 new flag variable group (8 variables)
- 7 new CLI flag definitions
- 2 functions updated to pass flags to bootstrap options
- 1 setup function enhanced with metadata extraction

**Lines of Code**:
- ~50 new lines added
- ~20 lines refactored
- 0 lines removed (backward compatible)

**Impact**: ✅ Fixes automated setup while maintaining full backward compatibility
