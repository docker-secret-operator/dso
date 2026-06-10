# Git Branch Strategy

This document describes the long-term branch strategy for DSO during and after CNCF review.

## Branch Hierarchy

```
main (stable, CNCF review)
  ↑
  │ (merges upward only)
  │
feature/web-ui (active development)
  ↑
  │ (merges upward only)
  │
advanced-platform (platform extensions)
  ↑
  │ (merges upward only)
  │
intelligence-pack (experimental features)
```

## Branch Purposes

### main
- **Purpose**: Production release branch under CNCF review
- **Status**: Stable, protected
- **Merge Policy**: Feature-complete PRs only, requires reviews
- **Protection**: Yes (no force-push, requires status checks)
- **Releases**: Tagged from this branch (semantic versioning)

### feature/web-ui
- **Purpose**: Active development branch
- **Status**: Unstable, rapid changes
- **Merge Policy**: Any developer can commit; code reviews optional but recommended
- **Protection**: No (allows force-push for history rewriting)
- **Duration**: Long-lived, never deleted

### advanced-platform
- **Purpose**: Platform extensions (policy, drift, graph)
- **Status**: Semi-stable, staging for platform features
- **Merge Policy**: Code review required; platform team approval
- **Protection**: Yes (requires reviews)
- **Duration**: Long-lived, never deleted

### intelligence-pack
- **Purpose**: Experimental features (correlation, recommendation, forecast, autonomy)
- **Status**: Experimental, subject to breaking changes
- **Merge Policy**: Code review required; research team approval
- **Protection**: Yes (requires reviews)
- **Duration**: Long-lived, never deleted

## Merge Direction

**Golden Rule**: Always merge upward, never downward or sideways.

### Valid Merges

```
main → feature/web-ui        ✓ (releases to development)
feature/web-ui → advanced-platform  ✓ (platform features to staging)
advanced-platform → intelligence-pack ✓ (tested features to experimental)
```

### Invalid Merges

```
feature/web-ui → main        ✗ (development to release)
advanced-platform → feature/web-ui ✗ (backward merge)
intelligence-pack → advanced-platform ✗ (experimental to stable)
intelligence-pack → main     ✗ (experimental to release)
```

## Update Frequency

### main → feature/web-ui
- **When**: After a release or significant feature completion
- **Frequency**: 1-2 times per month
- **How**: Standard merge commit, preserves history

### feature/web-ui → advanced-platform
- **When**: Weekly or after major feature work
- **Frequency**: Weekly or on-demand
- **How**: Standard merge commit, preserves history

### advanced-platform → intelligence-pack
- **When**: Weekly or after platform feature stabilization
- **Frequency**: Weekly or on-demand
- **How**: Standard merge commit, preserves history

## Cherry-Pick Policy

**No cherry-picking allowed.**

If a fix is needed across branches:

1. Fix in the lowest applicable branch (usually `feature/web-ui`)
2. Merge upward through the entire chain
3. Ensure all intermediate branches get the fix

Example:

```
Bug found in feature/web-ui
  ↓
Fix applied to feature/web-ui
  ↓
Merge to advanced-platform (picks up fix)
  ↓
Merge to intelligence-pack (picks up fix)
```

## Branch Protection Rules

### main
- Require pull request reviews (at least 2)
- Require status checks to pass (build, tests)
- Dismiss stale pull request approvals
- Require linear history
- Block force-push

### advanced-platform
- Require pull request reviews (at least 1)
- Require status checks to pass (build, tests)
- Allow force-push (for history rewriting)

### intelligence-pack
- Require pull request reviews (at least 1)
- Require status checks to pass (build, tests)
- Allow force-push (for history rewriting)

### feature/web-ui
- No protection (rapid development)
- No review requirement
- Allow force-push

## Workflow Examples

### Scenario 1: Fix a Bug in feature/web-ui

```bash
git checkout feature/web-ui
git pull origin feature/web-ui
git fix-branch bug-fix-123
# ... make changes ...
git add .
git commit -m "Fix: bug description"
git push origin bug-fix-123
# Create PR to feature/web-ui
# After merge:
git checkout advanced-platform
git merge feature/web-ui
git push origin advanced-platform
# Repeat for intelligence-pack
```

### Scenario 2: Add Policy Engine Feature (Advanced)

```bash
git checkout advanced-platform
git pull origin advanced-platform
git checkout -b feature/policy-enforcement
# ... make changes in internal/policy/ ...
git add .
git commit -m "Feature: policy enforcement rules"
git push origin feature/policy-enforcement
# Create PR to advanced-platform (requires review)
# After merge, propagate upward:
git checkout intelligence-pack
git merge advanced-platform
git push origin intelligence-pack
```

### Scenario 3: Add Intelligence Feature

```bash
git checkout intelligence-pack
git pull origin intelligence-pack
git checkout -b feature/autonomy-safety
# ... make changes in internal/autonomy/ ...
git add .
git commit -m "Feature: autonomous operation safety gates"
git push origin feature/autonomy-safety
# Create PR to intelligence-pack (requires review)
# No upward propagation (end of chain)
```

### Scenario 4: Release Process

```bash
# Prepare release in feature/web-ui
git checkout feature/web-ui
git tag -a v3.6.0 -m "Release v3.6.0"
git push origin v3.6.0

# Merge to main for CNCF
git checkout main
git pull origin main
git merge feature/web-ui
git push origin main
# Verify CNCF checks pass

# Propagate upward
git checkout advanced-platform
git merge feature/web-ui
git push origin advanced-platform

git checkout intelligence-pack
git merge advanced-platform
git push origin intelligence-pack
```

## Conflict Resolution

When conflicts occur during upward merges:

1. **Prefer upstream changes** (lower branch is source of truth)
2. **Document conflicts** in commit message
3. **Review carefully** to ensure no functionality is lost
4. **Test thoroughly** after conflict resolution

Example merge commit:

```
Merge feature/web-ui into advanced-platform

Merged upward to propagate bug fixes and improvements.
Resolved conflicts in:
  - internal/api/handlers.go (kept upstream API changes)
  - internal/storage/sqlite/migrations.go (appended new migrations)
```

## Branch Lifecycle

### Creating a New Branch

```bash
# From advanced-platform (if building platform features)
git checkout advanced-platform
git pull origin advanced-platform
git checkout -b feature/my-feature
# OR from intelligence-pack (if building experimental)
git checkout intelligence-pack
git pull origin intelligence-pack
git checkout -b feature/my-feature
```

### Deleting a Branch

**Never delete** `main`, `feature/web-ui`, `advanced-platform`, or `intelligence-pack`.

Short-lived feature branches can be deleted after merge:

```bash
git push origin --delete feature/my-feature
git branch -d feature/my-feature
```

## Monitoring and Auditing

### Branch Health Checks

Run weekly:

```bash
# Check for stale branches
git for-each-ref --sort=-committerdate refs/remotes/origin

# Check divergence from main
git log --oneline main..advanced-platform | wc -l
git log --oneline main..feature/web-ui | wc -l

# Check commit history
git log --oneline --graph main feature/web-ui advanced-platform intelligence-pack
```

### Divergence Tolerance

Acceptable divergence between branches:

| Branch Pair | Max Commits Behind | Action |
|-------------|-------------------|--------|
| feature/web-ui vs main | 30 | Merge main to sync |
| advanced-platform vs feature/web-ui | 20 | Merge feature/web-ui |
| intelligence-pack vs advanced-platform | 20 | Merge advanced-platform |

## Troubleshooting

### Branch is far behind upstream

```bash
git fetch origin
git rebase origin/main  # or origin/feature/web-ui
# If conflicts, resolve and continue
git rebase --continue
git push origin branch-name --force-with-lease
```

### Accidental merge in wrong direction

If you accidentally merge `advanced-platform` to `feature/web-ui`:

```bash
git revert -m 1 <merge-commit-hash>
git push origin feature/web-ui
```

### Need to cherry-pick across branches (emergency only)

This breaks the layering strategy but is acceptable in emergencies:

```bash
git cherry-pick <commit-hash>
git push origin branch-name
# Document in commit message why cherry-pick was necessary
# Plan to propagate properly in next merge cycle
```

## Tools and Automation

Recommended tools for managing multi-branch workflow:

- **GitKraken**: Visual multi-branch management
- **GitHub CLI**: `gh pr create`, `gh pr merge`
- **Git Aliases**: Custom commands for common workflows
- **GitHub Actions**: Automated testing, status checks

## Communication

- **Pull Requests**: Use branch name in PR title (e.g., "[advanced-platform] Feature X")
- **Commit Messages**: Reference branch when doing cross-branch work
- **Channel**: `#dso-architecture` for branch discussions
