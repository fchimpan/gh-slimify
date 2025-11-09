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

> [!TIP] 
> 💡 Wait, couldn't you just copy-paste the following prompt into AI agent and skip using this tool altogether? 🤔😏 *Spoiler alert: You'll be back.*
> ```md
> Goal: For every workflow file under `.github/workflows`, migrate jobs that currently run on `ubuntu-latest` to the container-based runner `ubuntu-slim`. Use the following decision rules in order when judging whether to migrate a job:
> 
> 1. Only consider jobs (including matrix entries) whose `runs-on` is `ubuntu-latest` or `ubuntu-24.04`.
> 2. Skip any job that uses service containers (`jobs.<job_id>.services`).
> 3. Skip any job already running inside a container (`jobs.<job_id>.container`).
> 4. Skip any job whose setup steps provision an environment that assumes a non-container host.
> 5. Skip any job whose run scripts rely on host-only commands or elevated system privileges that containers cannot provide (e.g., `systemctl`, `systemd`, etc.).
> 6. Skip any job whose execution time exceeds 15 minutes. Use the GitHub CLI to check the duration of the most recent successful run. Example commands:
> 
>    ```bash
>    # Get the database ID of the latest successful run
>    id=$(gh run list \
>      --repo ${owner}/${repo} \
>      --workflow ${workflow_file_name} \
>      --status success \
>      --limit 1 \
>      --json databaseId | jq .'[0].databaseId')
> 
>    # List jobs from that run to inspect start/completion times
>    gh api \
>      repos/{owner}/{repo}/actions/runs/${id}/jobs | jq '.jobs[] | {name: .name, started_at: .started_at, completed_at: .completed_at}'

> 
> Based on these rules, review each workflow and migrate every eligible job to ubuntu-slim. Afterward, report both the jobs that were successfully migrated and, for those that were not, the specific reasons they were ineligible.
> ```

> [!NOTE]
> At the time of writing, GitHub has not officially published a list of tools pre-installed on `ubuntu-slim` runners. Therefore, the tool detection for missing commands is **uncertain** and based on assumptions. The tool may incorrectly flag commands as missing (false positives) or miss commands that are actually missing (false negatives). Always verify manually before migrating critical workflows.

## 🚀 Quick Start

> [!IMPORTANT]
> All commands must be executed from the **repository root directory** (where `.github/workflows/` is located).

Get help:

```bash
$ gh slimify --help
```

### Scan Workflows

Scan specific workflow file(s) to find migration candidates:

```bash
gh slimify .github/workflows/ci.yml
```

Or scan multiple workflow files:

```bash
gh slimify .github/workflows/ci.yml .github/workflows/test.yml
```

To scan all workflows in `.github/workflows/`, use the `--all` flag:

```bash
gh slimify --all
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
  ❌ Cannot migrate (2 job(s)):
     • "docker-build" (L25)
       ❌ uses Docker commands
       .github/workflows/lint.yml:25
     • "test-with-db" (L35)
       ❌ uses service containers
       .github/workflows/lint.yml:35

✅ 1 job(s) can be safely migrated
⚠️  1 job(s) can be migrated but require attention
❌ 2 job(s) cannot be migrated
📊 Total: 2 job(s) eligible for migration
```

The output shows:
- **✅ Safe to migrate**: Jobs with no missing commands and known execution time
- **⚠️ Can migrate but requires attention**: Jobs with missing commands or unknown execution time
- **❌ Cannot migrate**: Jobs that cannot be migrated with specific reasons (e.g., uses Docker commands, uses service containers, uses container syntax, does not run on ubuntu-latest)
- **Warning reasons**: Displayed in a single line for easy understanding
- **Relative file paths**: Clickable links that work in VS Code, iTerm2, and other terminal emulators

### Auto-Fix Workflows

Automatically update eligible jobs to use `ubuntu-slim`. By default, only safe jobs (no missing commands and known execution time) are updated.

Specify workflow file(s):

```bash
gh slimify fix .github/workflows/ci.yml
```

Or use `--all` to fix all workflows:

```bash
gh slimify fix --all
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
gh slimify fix --force
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
gh slimify --all
```

### Using --file Flag

You can also use the `--file` (or `-f`) flag to specify workflow files:

```bash
gh slimify -f .github/workflows/ci.yml -f .github/workflows/test.yml
```

### Skip Duration Check

Skip fetching job durations from GitHub API. This is useful for:
- **API rate limit management**: Avoid hitting GitHub API rate limits when scanning many workflows
- **Faster scans**: Skip API calls for quicker results
- **When API access is unavailable**: Use when GitHub API is not accessible

```bash
gh slimify --skip-duration
```

Use the `--verbose` flag to enable debug output, which can help troubleshoot issues with API calls or workflow parsing:

```bash
gh slimify --verbose
```

### Force Update Jobs with Warnings

Update jobs with warnings (missing commands or unknown execution time):

```bash
gh slimify fix --force
```

### Combine Options

```bash
gh slimify fix .github/workflows/ci.yml --skip-duration --force
gh slimify --all --skip-duration
gh slimify fix --all --force
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
> **Setup Action Detection**: If a job uses popular setup actions from GitHub Marketplace (e.g., `actions/setup-go`,`hashicorp/setup-terraform`), the commands provided by those actions (e.g., `go`, `terraform`) will **not** be flagged as missing. This is because these setup actions install the necessary tools, making the job safe to migrate. The tool recognizes setup actions from GitHub Marketplace's verified creators, including official GitHub actions and popular third-party actions.

If any condition is violated, the job will **not** be migrated.

### Job Status Classification

Jobs are classified into three categories:

- **✅ Safe to migrate**: No missing commands and execution time is known
- **⚠️ Can migrate but requires attention**: Has missing commands or execution time is unknown
- **❌ Cannot migrate**: Does not meet migration criteria (e.g., uses Docker commands, uses service containers, uses container syntax, does not run on ubuntu-latest)

Missing commands are tools that exist in `ubuntu-latest` but need to be installed in `ubuntu-slim` (e.g., `nvm`). These jobs can still be migrated, but you may need to add setup steps to install the required tools.

When a job cannot be migrated, the specific reason(s) are displayed, such as:
- "does not run on ubuntu-latest"
- "uses Docker commands"
- "uses container-based GitHub Actions"
- "uses service containers"
- "uses container syntax"

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
5. **Classify Jobs**: Separates jobs into "safe" (no warnings), "requires attention" (has warnings), and "cannot migrate" (does not meet criteria) categories
6. **Report Results**: Displays eligible jobs grouped by status with:
   - Visual indicators (✅ for safe, ⚠️ for warnings, ❌ for ineligible)
   - Ineligibility reasons for jobs that cannot be migrated
   - Warning reasons in a single line
   - Relative file paths with line numbers (clickable in most terminals)
   - Execution durations
7. **Auto-Fix** (optional): Updates `runs-on: ubuntu-latest` to `runs-on: ubuntu-slim`:
   - By default: Only safe jobs are updated
   - With `--force`: All eligible jobs (including those with warnings) are updated


## 📄 License

MIT License
