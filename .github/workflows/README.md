# GitHub Actions Workflows

This repository contains CI/CD workflows for testing and building the workload-variant-autoscaler.

## Workflows

### 1. CI - Manual Trigger (ci-manual-trigger.yaml)

**Purpose**: Run tests on-demand to save GitHub Actions minutes on private repositories.

**How to Use**:

1. Go to your GitHub repository
2. Click on the **Actions** tab
3. Select **"CI - Manual Trigger (All Tests)"** from the left sidebar
4. Click **"Run workflow"** button (top right)
5. Fill in the parameters:
   - **branch**: Branch name to test (optional, defaults to current branch)
   - **pr_number**: PR number to test (optional, takes precedence over branch)
   - **skip_e2e**: Check this to skip E2E tests and save ~30 minutes

**Examples**:

- **Test current branch with all tests**:
  - Leave all fields empty
  - Uncheck "skip_e2e"
  - Click "Run workflow"

- **Test a specific branch**:
  - branch: `refactor/single-variant-arch-clean`
  - Leave pr_number empty
  - Uncheck "skip_e2e"

- **Test a PR**:
  - pr_number: `123`
  - Leave branch empty
  - Uncheck "skip_e2e"

- **Quick lint + unit tests only** (saves ~30 minutes):
  - Leave branch/pr_number as needed
  - **Check "skip_e2e"** ✓

**What it runs**:

1. ✅ Lint (golangci-lint)
2. ✅ Unit tests (`make test`)
3. ✅ Build (`make build`)
4. ✅ E2E tests (`make test-e2e`) - ~30 minutes (optional)

**Estimated Duration**:
- With E2E: ~35-40 minutes
- Without E2E: ~5-8 minutes

**GitHub Actions Minutes Used** (Private Repo):
- Full run: ~40 minutes
- Skip E2E: ~8 minutes

---

### 2. CI - PR Checks (ci-pr-checks.yaml)

**Purpose**: Original automatic PR checks (currently needs Kind installation fixes).

**Trigger**: Automatically runs on PRs to `main` or `dev` branches.

**Status**: ⚠️ Needs updates to install Kind/kubectl (currently will fail on E2E tests).

**Note**: Consider using the manual trigger workflow instead to save minutes on private repos.

---

### 3. CI - Release (ci-release.yaml)

**Purpose**: Release automation workflow.

**Trigger**: TBD (check file for details)

---

## Best Practices for Private Repos

To maximize your **2,000 free minutes/month** on private repositories:

1. **Use manual trigger workflow** for most testing
2. **Skip E2E tests** during development (use `skip_e2e: true`)
3. **Run full E2E tests** only before merging PRs
4. **Test locally** with `make test` and `make lint` before pushing

### Estimated Monthly Usage

**Conservative approach**:
- 20 quick checks (no E2E): 20 × 8 min = 160 minutes
- 5 full E2E runs: 5 × 40 min = 200 minutes
- **Total: ~360 minutes/month** (well within limit)

**Aggressive approach** (if needed):
- 40 quick checks: 40 × 8 min = 320 minutes
- 10 full E2E runs: 10 × 40 min = 400 minutes
- **Total: ~720 minutes/month** (still within limit)

---

## Local Testing

Before triggering CI workflows, test locally to catch issues early:

```bash
# Lint
make lint

# Unit tests (fast)
make test

# Build
make build

# E2E tests (requires Kind)
make test-e2e
```

---

## Troubleshooting

**Q: Workflow not showing up in Actions tab?**
- Make sure you've pushed the workflow file to GitHub
- Check that GitHub Actions is enabled: Settings → Actions → Allow actions

**Q: E2E tests failing?**
- Ensure Kind and kubectl are installed (manual trigger workflow installs them automatically)
- Check Docker is available (GitHub runners have it pre-installed)

**Q: Running out of minutes?**
- Use `skip_e2e: true` more often during development
- Consider making repository public (unlimited minutes)
- Test locally before running CI

**Q: How to test a PR from a fork?**
- Use the PR number in the `pr_number` input field
- GitHub will automatically fetch and test the PR branch
