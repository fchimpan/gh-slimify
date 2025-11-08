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

## ✨ Benefits

- **💰 Cost Savings**: `ubuntu-slim` runners are optimized for lightweight jobs, reducing CI costs
- **⚡ Faster Startup**: Slim runners start faster than standard runners
- **🛡️ Safe Migration**: Automatically detects incompatible patterns (Docker, services, containers)
- **📊 Smart Analysis**: Checks actual job durations from GitHub API to ensure compatibility
- **🎯 Precise Updates**: Shows exact file paths and line numbers for each candidate job
- **🔄 One-Click Fix**: Automatically updates workflows with `gh slimfy fix`

## 📦 Installation

Install as a GitHub CLI extension:

```bash
gh extension install fchimpan/gh-slimify
```

## 🚀 Quick Start


```bash
$ gh slimfy --help
slimfy is a GitHub CLI extension that automatically detects and safely migrates
eligible ubuntu-latest jobs to ubuntu-slim.

It analyzes .github/workflows/*.yml files and identifies jobs that can be safely
migrated based on migration criteria.

Usage:
  slimfy [flags]
  slimfy [command]

Available Commands:
  completion  Generate the autocompletion script for the specified shell
  fix         Automatically update workflows to use ubuntu-slim
  help        Help about any command

Flags:
  -f, --file stringArray   Specify workflow file(s) to process. If not specified, all files in .github/workflows/*.yml are processed. Can be specified multiple times (e.g., -f .github/workflows/ci.yml -f .github/workflows/test.yml)
  -h, --help               help for slimfy
      --skip-duration      Skip fetching job execution durations from GitHub API to avoid unnecessary API calls

Use "slimfy [command] --help" for more information about a command.
```

### Scan Workflows

Scan all workflows in `.github/workflows/` to find migration candidates:

```bash
gh slimfy
```

**Example Output:**

```
.github/workflows/lint.yml
  - job "lint" (L8) → ubuntu-slim compatible (last run: 4m) /path/to/.github/workflows/lint.yml:8

.github/workflows/test.yml
  - job "unit-test" (L12) → ubuntu-slim compatible (last run: 9m) /path/to/.github/workflows/test.yml:12

Total: 2 job(s) can be safely migrated.
```

### Auto-Fix Workflows

Automatically update eligible jobs to use `ubuntu-slim`:

```bash
gh slimfy fix
```

**Example Output:**

```
Updating workflows to use ubuntu-slim...

Updating .github/workflows/lint.yml
  ✓ Updated job "lint" (L8) → ubuntu-slim

Updating .github/workflows/test.yml
  ✓ Updated job "unit-test" (L12) → ubuntu-slim

Successfully updated 2 job(s) to use ubuntu-slim.
```

## 📖 Usage

### Scan Specific Workflows

Scan only specific workflow files:

```bash
gh slimfy -f .github/workflows/ci.yml -f .github/workflows/test.yml
```

### Skip Duration Check

Skip fetching job durations from GitHub API (useful for faster scans or when API access is unavailable):

```bash
gh slimfy --skip-duration
```

### Combine Options

```bash
gh slimfy fix -f .github/workflows/ci.yml --skip-duration
```

## 🔍 Migration Criteria

A job is eligible for migration to `ubuntu-slim` if **all** of the following conditions are met:

1. ✅ Runs on `ubuntu-latest`
2. ✅ Does **not** use Docker commands (`docker build`, `docker run`, `docker compose`, etc.)
3. ✅ Does **not** use Docker-based GitHub Actions (e.g., `docker/build-push-action`, `docker/login-action`)
4. ✅ Does **not** use `services:` containers (PostgreSQL, Redis, MySQL, etc.)
5. ✅ Does **not** use `container:` syntax (jobs running inside Docker containers)
6. ✅ Latest workflow run duration is **under 15 minutes**

If any condition is violated, the job will **not** be migrated.

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
3. **Fetch Durations**: Retrieves latest job execution times from GitHub API (unless `--skip-duration` is used)
4. **Report Results**: Displays eligible jobs with file paths, line numbers, and execution durations
5. **Auto-Fix** (optional): Updates `runs-on: ubuntu-latest` to `runs-on: ubuntu-slim` for safe jobs

## 🔗 Links

- **File Links**: Output includes clickable file links (e.g., `file://path/to/workflow.yml:8`) that work in VS Code, iTerm2, and other terminal emulators
- **GitHub API**: Automatically uses your `gh` CLI authentication for API calls

## 🤝 Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## 📄 License

MIT License - see [LICENSE](LICENSE) file for details.

## 🙏 Acknowledgments

- Built for the GitHub Actions community
- Inspired by the need for cost-efficient CI/CD workflows
- Uses [Cobra](https://github.com/spf13/cobra) for CLI functionality

---

**Made with ❤️ for faster, cheaper CI**

