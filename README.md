# gh-slimify

[![Go Version](https://img.shields.io/badge/go-1.25.3-blue.svg)](https://golang.org)
[![License](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)
[![GitHub CLI](https://img.shields.io/badge/gh-cli-blue.svg)](https://cli.github.com)

>[!WARNING] 
>`ubuntu-slim` is currently in **public preview** and may change before general availability. 
>Please review GitHub's official documentation for the latest updates and breaking changes.

## 🎯 Motivation

GitHub Actions recently introduced the lightweight `ubuntu-slim` runner (1 vCPU / 5 GB RAM, max 15 min runtime) as a cost-efficient alternative to `ubuntu-latest`. However, manually identifying which workflows can safely migrate is tedious and error-prone:

- ❌ Jobs using Docker commands or containers cannot migrate
- ❌ Jobs using `services:` containers are incompatible
- ❌ Jobs exceeding 15 minutes will fail
- ❌ Container-based GitHub Actions are not supported

**`gh-slimify` automates this entire process**, analyzing your workflows and safely migrating eligible jobs with a single command.

## 📦 Installation

Install as a GitHub CLI extension:

```bash
gh extension install fchimpan/gh-slimify
```

## 🚀 Quick Start

> [!IMPORTANT]
> All commands must be executed from the **repository root directory** (where `.github/workflows/` is located).

Get help:

```bash
$ gh slimfy --help
```

### Scan Workflows

Scan specific workflow file(s) to find migration candidates:

```bash
gh slimfy .github/workflows/ci.yml
```

Or scan multiple workflow files:

```bash
gh slimfy .github/workflows/ci.yml .github/workflows/test.yml
```

To scan all workflows in `.github/workflows/`, use the `--all` flag:

```bash
gh slimfy --all
```

**Example Output:**

```
📄 .github/workflows/lint.yml
  ✅ Safe to migrate (1 job(s)):
     • "lint" (L8) - Last execution time: 4m
       .github/workflows/lint.yml:8
  ⚠️  Can migrate but requires attention (1 job(s)):
     • "build" (L15)
       ⚠️  Setup may be required (go), Last execution time: unknown
       .github/workflows/lint.yml:15

✅ 1 job(s) can be safely migrated
⚠️  1 job(s) can be migrated but require attention
📊 Total: 2 job(s) eligible for migration
```

The output shows:
- **✅ Safe to migrate**: Jobs with no missing commands and known execution time
- **⚠️ Can migrate but requires attention**: Jobs with missing commands or unknown execution time
- **Warning reasons**: Displayed in a single line for easy understanding
- **Relative file paths**: Clickable links that work in VS Code, iTerm2, and other terminal emulators

### Auto-Fix Workflows

Automatically update eligible jobs to use `ubuntu-slim`. By default, only safe jobs (no missing commands and known execution time) are updated.

Specify workflow file(s):

```bash
gh slimfy fix .github/workflows/ci.yml
```

Or use `--all` to fix all workflows:

```bash
gh slimfy fix --all
```

**Example Output (default - safe jobs only):**

```
Updating workflows to use ubuntu-slim (safe jobs only)...
Skipping 1 job(s) with warnings. Use --force to update them.

Updating .github/workflows/lint.yml
  ✓ Updated job "lint" (L8) → ubuntu-slim

Successfully updated 1 job(s) to use ubuntu-slim.
```

To also update jobs with warnings (missing commands or unknown execution time), use the `--force` flag:

```bash
gh slimfy fix --force
```

**Example Output (with --force):**

```
Updating workflows to use ubuntu-slim (including jobs with warnings)...

Updating .github/workflows/lint.yml
  ⚠️  Updated job "build" (L15) → ubuntu-slim (with warnings)
  ✓ Updated job "lint" (L8) → ubuntu-slim

Successfully updated 2 job(s) to use ubuntu-slim.
```

## 📖 Usage

### Scan All Workflows

To scan all workflows in `.github/workflows/`, use the `--all` flag:

```bash
gh slimfy --all
```

### Using --file Flag

You can also use the `--file` (or `-f`) flag to specify workflow files:

```bash
gh slimfy -f .github/workflows/ci.yml -f .github/workflows/test.yml
```

### Skip Duration Check

Skip fetching job durations from GitHub API. This is useful for:
- **API rate limit management**: Avoid hitting GitHub API rate limits when scanning many workflows
- **Faster scans**: Skip API calls for quicker results
- **When API access is unavailable**: Use when GitHub API is not accessible

```bash
gh slimfy --skip-duration
```

Use the `--verbose` flag to enable debug output, which can help troubleshoot issues with API calls or workflow parsing:

```bash
gh slimfy --verbose
```

### Force Update Jobs with Warnings

Update jobs with warnings (missing commands or unknown execution time):

```bash
gh slimfy fix --force
```

### Combine Options

```bash
gh slimfy fix .github/workflows/ci.yml --skip-duration --force
gh slimfy --all --skip-duration
gh slimfy fix --all --force
```

## 🔍 Migration Criteria

A job is eligible for migration to `ubuntu-slim` if **all** of the following conditions are met:

1. ✅ Runs on `ubuntu-latest`
2. ✅ Does **not** use Docker commands (`docker build`, `docker run`, `docker compose`, etc.)
3. ✅ Does **not** use Docker-based GitHub Actions (e.g., `docker/build-push-action`, `docker/login-action`)
4. ✅ Does **not** use `services:` containers (PostgreSQL, Redis, MySQL, etc.)
5. ✅ Does **not** use `container:` syntax (jobs running inside Docker containers)
6. ✅ Latest workflow run duration is **under 15 minutes** (checked via GitHub API)
7. ⚠️ Jobs using commands that exist in `ubuntu-latest` but not in `ubuntu-slim` (e.g. `nvm`) will be flagged with warnings but are still eligible for migration. You may need to add setup steps to install these tools in `ubuntu-slim`.

> [!NOTE]
> The missing command detection is not complete. It cannot detect commands installed via setup actions (e.g., `actions/setup-go@v5`). These actions typically install tools that may not be available in `ubuntu-slim` by default, so manual verification is recommended.

If any condition is violated, the job will **not** be migrated.

### Job Status Classification

Jobs are classified into two categories:

- **✅ Safe to migrate**: No missing commands and execution time is known
- **⚠️ Can migrate but requires attention**: Has missing commands or execution time is unknown

Missing commands are tools that exist in `ubuntu-latest` but need to be installed in `ubuntu-slim` (e.g., `nvm`). These jobs can still be migrated, but you may need to add setup steps to install the required tools.

## 📝 Examples

### Example 1: Simple Lint Job ✅

```yaml
jobs:
  lint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
      - run: npm run lint
```

**Result:** ✅ Eligible — No Docker, services, or containers

### Example 2: Docker Build Job ❌

```yaml
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: docker/build-push-action@v6
        with:
          context: .
          push: true
```

**Result:** ❌ Not eligible — Uses Docker-based action

### Example 3: Job with Services ❌

```yaml
jobs:
  test:
    runs-on: ubuntu-latest
    services:
      postgres:
        image: postgres:14
    steps:
      - run: npm test
```

**Result:** ❌ Not eligible — Uses `services:` containers

### Example 4: Container Job ❌

```yaml
jobs:
  test:
    runs-on: ubuntu-latest
    container:
      image: node:18
    steps:
      - run: node --version
```

**Result:** ❌ Not eligible — Uses `container:` syntax

## 🛠️ How It Works

1. **Parse Workflows**: Scans `.github/workflows/*.yml` files and parses job definitions
2. **Check Criteria**: Evaluates each job against migration criteria (Docker, services, containers)
3. **Detect Missing Commands**: Identifies commands used in jobs that exist in `ubuntu-latest` but not in `ubuntu-slim`
4. **Fetch Durations**: Retrieves latest job execution times from GitHub API (unless `--skip-duration` is used)
5. **Classify Jobs**: Separates jobs into "safe" (no warnings) and "requires attention" (has warnings) categories
6. **Report Results**: Displays eligible jobs grouped by status with:
   - Visual indicators (✅ for safe, ⚠️ for warnings)
   - Warning reasons in a single line
   - Relative file paths with line numbers (clickable in most terminals)
   - Execution durations
7. **Auto-Fix** (optional): Updates `runs-on: ubuntu-latest` to `runs-on: ubuntu-slim`:
   - By default: Only safe jobs are updated
   - With `--force`: All eligible jobs (including those with warnings) are updated


## 📄 License

MIT License
