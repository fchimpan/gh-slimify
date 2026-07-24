package workflow

import (
	"maps"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// setupActionCommands maps setup actions to the commands they provide.
// When a setup action is present in a job, these commands should not be
// reported as missing even if they're not in ubuntu-slim by default.
//
// This includes:
// - GitHub official setup actions (actions/setup-*)
// - Verified creator setup actions from GitHub Marketplace
//
// References:
// - https://github.com/marketplace?query=setup&verification=verified_creator&type=actions
var setupActionCommands = map[string][]string{
	"actions/setup-go":              {"go"},
	"actions/setup-node":            {"node", "npm", "npx"},
	"actions/setup-python":          {"python", "python3", "pip", "pip3"},
	"actions/setup-java":            {"java", "javac", "mvn", "gradle"},
	"actions/setup-dotnet":          {"dotnet"},
	"actions/setup-ruby":            {"ruby", "gem"},
	"hashicorp/setup-terraform":     {"terraform"},
	"hashicorp/setup-packer":        {"packer"},
	"oven-sh/setup-bun":             {"bun"},
	"astral-sh/setup-uv":            {"uv"},
	"erlef/setup-beam":              {"erl", "elixir", "mix", "rebar3", "hex"},
	"microsoft/setup-msbuild":       {"msbuild"},
	"denoland/setup-deno":           {"deno"},
	"jfrog/setup-jfrog-cli":         {"jfrog"},
	"supabase/setup-cli":            {"supabase"},
	"aws-actions/setup-sam":         {"sam"},
	"gruntwork-io/setup-terragrunt": {"terragrunt"},
	"pdm-project/setup-pdm":         {"pdm"},
}

// containerCommands lists container-tool commands that require a Docker daemon
// or privileges that ubuntu-slim's unprivileged container cannot provide.
// Matching happens on the command position of parsed shell statements, so any
// subcommand (build, buildx, load, save, ...) is covered.
var containerCommands = map[string]bool{
	"docker":         true,
	"docker-compose": true,
	"podman":         true,
	"podman-compose": true,
	"nerdctl":        true,
	"buildah":        true,
}

// privilegedCommands lists operations that require capabilities not available
// in non-privileged containers like ubuntu-slim.
// Categories: filesystem mounts, kernel modules, network firewall,
// sysctl, namespaces, cgroups, device management, Linux capabilities.
var privilegedCommands = map[string]bool{
	"mount":     true,
	"umount":    true,
	"modprobe":  true,
	"insmod":    true,
	"rmmod":     true,
	"iptables":  true,
	"ip6tables": true,
	"nft":       true,
	"nftables":  true,
	"sysctl":    true,
	"unshare":   true,
	"nsenter":   true,
	"cgcreate":  true,
	"cgexec":    true,
	"mknod":     true,
	"losetup":   true,
	"setcap":    true,
	"getcap":    true,
	"capsh":     true,
}

// wrapperCommands are prefix commands that execute their arguments; the
// wrapped command is the one that matters for detection.
var wrapperCommands = map[string]bool{
	"sudo":   true,
	"env":    true,
	"time":   true,
	"nohup":  true,
	"setsid": true,
	"stdbuf": true,
}

// interpreterCommands execute their arguments, stdin, or heredocs as shell
// code. Literal payloads passed to them are scanned with the fallback
// patterns so container or privileged usage hidden inside (e.g.
// bash -c "docker build .") is not missed.
var interpreterCommands = map[string]bool{
	"bash":   true,
	"sh":     true,
	"zsh":    true,
	"eval":   true,
	"source": true,
	".":      true,
}

var (
	// fallbackContainerPattern approximates container-command detection when a
	// script cannot be parsed as shell (e.g. shell: python). Broad on purpose:
	// for the cannot-migrate checks a false positive is safer than migrating a
	// job that needs a Docker daemon.
	fallbackContainerPattern = commandAlternationPattern(containerCommands)

	// privilegedCommandPattern is the parse-failure fallback for privileged
	// operation detection.
	privilegedCommandPattern = commandAlternationPattern(privilegedCommands)

	// githubExpressionPattern matches ${{ ... }} expressions, which are not
	// valid shell until GitHub substitutes them at run time.
	githubExpressionPattern = regexp.MustCompile(`(?s)\$\{\{.*?\}\}`)

	// containerActionPrefixes lists prefixes that indicate container-based GitHub Actions
	// This covers:
	// - docker:// image syntax (e.g., "docker://alpine:latest")
	// - docker/ organization actions (e.g., "docker/build-push-action@v6")
	// Future additions could include: "container://", "podman/", etc.
	containerActionPrefixes = []string{"docker://", "docker/"}
)

// commandAlternationPattern builds a word-boundary alternation matching any
// key of a command set, so the parse-failure fallbacks share one source of
// truth with the AST matchers. Longer names come first so captures prefer the
// full command (nftables over nft).
func commandAlternationPattern(cmds map[string]bool) *regexp.Regexp {
	names := slices.Sorted(maps.Keys(cmds))
	sort.SliceStable(names, func(i, j int) bool { return len(names[i]) > len(names[j]) })
	for i, name := range names {
		names[i] = regexp.QuoteMeta(name)
	}
	return regexp.MustCompile(`\b(` + strings.Join(names, "|") + `)\b`)
}

// IsContainerActionRef reports whether a uses: reference is container-based
// by its prefix alone: docker:// images and docker/ organization actions.
func IsContainerActionRef(ref string) bool {
	for _, prefix := range containerActionPrefixes {
		if strings.HasPrefix(ref, prefix) {
			return true
		}
	}
	return false
}

// migratableRunners are the GitHub-hosted runner labels that ubuntu-slim can
// replace. ubuntu-latest currently resolves to ubuntu-24.04, the same OS
// release ubuntu-slim is based on.
var migratableRunners = map[string]bool{
	"ubuntu-latest": true,
	"ubuntu-24.04":  true,
}

// runnerLabels returns the string labels of runs-on, handling both the
// scalar and array forms.
func (j *Job) runnerLabels() []string {
	switch v := j.RunsOn.(type) {
	case string:
		return []string{v}
	case []any:
		var labels []string
		for _, item := range v {
			if str, ok := item.(string); ok {
				labels = append(labels, str)
			}
		}
		return labels
	default:
		return nil
	}
}

// IsMigratableRunner checks if a job runs on a label that ubuntu-slim can
// replace (ubuntu-latest or ubuntu-24.04).
func (j *Job) IsMigratableRunner() bool {
	return slices.ContainsFunc(j.runnerLabels(), func(label string) bool {
		return migratableRunners[label]
	})
}

// IsUbuntuSlim checks if a job already runs on ubuntu-slim
func (j *Job) IsUbuntuSlim() bool {
	return slices.Contains(j.runnerLabels(), "ubuntu-slim")
}

// HasExpressionRunsOn reports whether runs-on is set by a ${{ }} expression
// (typically a matrix), which this tool does not evaluate.
func (j *Job) HasExpressionRunsOn() bool {
	s, ok := j.RunsOn.(string)
	return ok && strings.Contains(s, "${{")
}

// HasMultipleRunnerLabels reports whether runs-on is an array with more than
// one label. Multi-label runs-on targets self-hosted runners, which must not
// be rewritten to a single GitHub-hosted label.
func (j *Job) HasMultipleRunnerLabels() bool {
	labels, ok := j.RunsOn.([]any)
	return ok && len(labels) > 1
}

// scriptAnalysis is the result of parsing one run: script.
type scriptAnalysis struct {
	raw         string   // original script text
	commands    []string // command names in invocation position, wrappers unwrapped
	assigned    []string // literal string values assigned to variables
	payloads    []string // literal text handed to shell interpreters (bash -c, heredocs)
	parseFailed bool     // shell parse failed or shell: is not sh/bash
}

// stepAnalyses parses each run: script once and caches the results for the
// docker, privileged-operation, and missing-command checks.
func (j *Job) stepAnalyses() []scriptAnalysis {
	if j.analysesDone {
		return j.analyses
	}
	j.analysesDone = true
	for _, step := range j.Steps {
		if step.Run == "" {
			continue
		}
		j.analyses = append(j.analyses, analyzeScript(step.Run, step.Shell))
	}
	return j.analyses
}

// analyzeScript parses a run: script as shell and collects invoked command
// names and literal variable assignments. Scripts that cannot be parsed are
// marked parseFailed; callers fall back to pattern matching on the raw text.
func analyzeScript(script, shell string) scriptAnalysis {
	a := scriptAnalysis{raw: script}
	if !isShellSyntax(shell) {
		a.parseFailed = true
		return a
	}

	sanitized := githubExpressionPattern.ReplaceAllString(script, "GITHUB_EXPRESSION")
	parser := syntax.NewParser(syntax.Variant(syntax.LangBash))
	file, err := parser.Parse(strings.NewReader(sanitized), "")
	if err != nil {
		a.parseFailed = true
		return a
	}

	syntax.Walk(file, func(node syntax.Node) bool {
		switch n := node.(type) {
		case *syntax.Stmt:
			a.payloads = append(a.payloads, interpreterPayloads(n)...)
		case *syntax.CallExpr:
			a.commands = append(a.commands, commandNamesFromCall(n)...)
		case *syntax.Assign:
			if v, ok := literalString(n.Value); ok && v != "" {
				a.assigned = append(a.assigned, v)
			}
		}
		return true
	})
	return a
}

// interpreterPayloads collects the literal arguments and heredoc bodies of
// statements that invoke a shell interpreter, so the fallback patterns can
// scan code the syntax tree treats as plain data.
func interpreterPayloads(stmt *syntax.Stmt) []string {
	call, ok := stmt.Cmd.(*syntax.CallExpr)
	if !ok {
		return nil
	}
	names := commandNamesFromCall(call)
	if len(names) == 0 || !interpreterCommands[strings.ToLower(names[len(names)-1])] {
		return nil
	}

	var payloads []string
	for _, arg := range call.Args[1:] {
		if v := lenientString(arg); v != "" {
			payloads = append(payloads, v)
		}
	}
	for _, redir := range stmt.Redirs {
		if redir.Hdoc != nil {
			if v := lenientString(redir.Hdoc); v != "" {
				payloads = append(payloads, v)
			}
		}
	}
	return payloads
}

// isShellSyntax reports whether a step's shell: value selects a POSIX-ish
// shell that mvdan.cc/sh can parse. An empty value means the runner default,
// which is bash on Linux runners.
func isShellSyntax(shell string) bool {
	fields := strings.Fields(shell)
	if len(fields) == 0 {
		return true
	}
	switch filepath.Base(fields[0]) {
	case "bash", "sh":
		return true
	}
	return false
}

// commandNamesFromCall returns the command names invoked by a call expression,
// unwrapping prefix commands like sudo and env. Dynamic command words
// ($CMD, $(...)) are skipped; command substitutions inside them are still
// visited by the surrounding Walk.
func commandNamesFromCall(call *syntax.CallExpr) []string {
	var names []string
	args := call.Args
	for len(args) > 0 {
		name, ok := literalString(args[0])
		if !ok || name == "" {
			return names
		}
		name = normalizeCommand(name)
		names = append(names, name)
		if !wrapperCommands[strings.ToLower(name)] {
			return names
		}
		// Skip the wrapper's flags and VAR=value arguments to reach the
		// wrapped command.
		args = args[1:]
		for len(args) > 0 {
			s, ok := literalString(args[0])
			if !ok {
				return names
			}
			if strings.HasPrefix(s, "-") || strings.Contains(s, "=") {
				args = args[1:]
				continue
			}
			break
		}
	}
	return names
}

// lenientString renders the literal fragments of a word, skipping dynamic
// parts instead of failing. Suitable for pattern scanning, not for exact
// command names.
func lenientString(w *syntax.Word) string {
	if w == nil {
		return ""
	}
	var sb strings.Builder
	for _, part := range w.Parts {
		switch p := part.(type) {
		case *syntax.Lit:
			sb.WriteString(p.Value)
		case *syntax.SglQuoted:
			sb.WriteString(p.Value)
		case *syntax.DblQuoted:
			for _, dp := range p.Parts {
				if lit, ok := dp.(*syntax.Lit); ok {
					sb.WriteString(lit.Value)
				}
			}
		}
	}
	return sb.String()
}

// literalString renders a word composed solely of literal or quoted-literal
// parts. It returns ok=false when any part is dynamic (variables, command
// substitutions, globs).
func literalString(w *syntax.Word) (string, bool) {
	if w == nil {
		return "", false
	}
	var sb strings.Builder
	for _, part := range w.Parts {
		switch p := part.(type) {
		case *syntax.Lit:
			sb.WriteString(p.Value)
		case *syntax.SglQuoted:
			sb.WriteString(p.Value)
		case *syntax.DblQuoted:
			for _, dp := range p.Parts {
				lit, ok := dp.(*syntax.Lit)
				if !ok {
					return "", false
				}
				sb.WriteString(lit.Value)
			}
		default:
			return "", false
		}
	}
	return sb.String(), true
}

// HasDockerCommands checks if a job uses container-tool commands (docker,
// docker-compose, podman, ...) that require a daemon or privileges
// unavailable on ubuntu-slim. Detection matches the command position of
// parsed shell statements, so every subcommand (build, buildx, load, save,
// stop, ...) is covered, while occurrences inside string arguments or
// comments are not. Literal variable assignments such as CMD='docker build'
// are also checked.
func (j *Job) HasDockerCommands() bool {
	for _, a := range j.stepAnalyses() {
		if analysisUsesContainerTooling(a) {
			return true
		}
	}
	return false
}

// ScriptUsesContainerTooling reports whether a standalone shell script
// invokes container tooling, using the same analysis as HasDockerCommands.
// Used for the run: steps of composite actions.
func ScriptUsesContainerTooling(script string) bool {
	return analysisUsesContainerTooling(analyzeScript(script, ""))
}

// analysisUsesContainerTooling applies the container-tooling checks to one
// analyzed script.
func analysisUsesContainerTooling(a scriptAnalysis) bool {
	if a.parseFailed {
		return fallbackContainerPattern.MatchString(strings.ToLower(a.raw))
	}
	for _, cmd := range a.commands {
		if containerCommands[strings.ToLower(cmd)] {
			return true
		}
	}
	for _, v := range a.assigned {
		if startsWithContainerCommand(v) {
			return true
		}
	}
	for _, p := range a.payloads {
		if fallbackContainerPattern.MatchString(strings.ToLower(p)) {
			return true
		}
	}
	return false
}

// startsWithContainerCommand reports whether a literal string looks like a
// container-tool invocation (e.g. a value later executed via $CMD).
func startsWithContainerCommand(v string) bool {
	fields := strings.Fields(strings.ToLower(v))
	for len(fields) > 0 && wrapperCommands[fields[0]] {
		fields = fields[1:]
	}
	return len(fields) > 0 && containerCommands[normalizeCommand(fields[0])]
}

// HasContainerActions checks if a job uses container-based GitHub Actions
// It detects actions that use container prefixes defined in containerActionPrefixes:
// - docker:// image syntax (e.g., "docker://alpine:latest")
// - docker/ organization actions (e.g., "docker/build-push-action@v6")
// Future container tools can be added by extending containerActionPrefixes.
func (j *Job) HasContainerActions() bool {
	for _, step := range j.Steps {
		if step.Uses != "" && IsContainerActionRef(step.Uses) {
			return true
		}
	}
	return false
}

// HasServices checks if a job uses services
// Services are containers that are shared between jobs.
// Since ubuntu-slim runs itself inside a container and does not provide dockerd,
// nested container jobs are not supported.
func (j *Job) HasServices() bool {
	return j.Services != nil
}

// HasPrivilegedOperations checks if a job invokes privileged operations
// that require capabilities not available in non-privileged containers.
// Matching happens on the command position of parsed shell statements, so
// words inside string arguments (e.g. echo "amount") do not match.
// Returns whether privileged operations were found and a deduplicated list of
// command names.
func (j *Job) HasPrivilegedOperations() (bool, []string) {
	seen := make(map[string]bool)
	var cmds []string
	record := func(cmd string) {
		if !seen[cmd] {
			seen[cmd] = true
			cmds = append(cmds, cmd)
		}
	}

	for _, a := range j.stepAnalyses() {
		if a.parseFailed {
			for _, match := range privilegedCommandPattern.FindAllStringSubmatch(strings.ToLower(a.raw), -1) {
				record(match[1])
			}
			continue
		}
		for _, cmd := range a.commands {
			if lower := strings.ToLower(cmd); privilegedCommands[lower] {
				record(lower)
			}
		}
		for _, p := range a.payloads {
			for _, match := range privilegedCommandPattern.FindAllStringSubmatch(strings.ToLower(p), -1) {
				record(match[1])
			}
		}
	}

	return len(cmds) > 0, cmds
}

// HasContainer checks if a job uses the container: syntax
// Jobs with container: run steps inside a Docker container, which requires
// access to the Docker daemon. Since ubuntu-slim runs itself inside a container
// and does not provide dockerd, nested container jobs are not supported.
func (j *Job) HasContainer() bool {
	return j.Container != nil
}

// GetMissingCommands extracts commands from job steps and returns a list of commands
// that exist in ubuntu-latest but are missing in ubuntu-slim.
// Commands are taken from the invocation position of parsed shell statements;
// scripts that cannot be parsed fall back to a line-based extraction.
// Commands provided by setup actions (e.g., setup-go provides "go") are excluded
// from the missing commands list since they will be available after the setup action runs.
func (j *Job) GetMissingCommands() []string {
	if !j.IsMigratableRunner() {
		// Only check commands for jobs that could migrate
		return nil
	}

	// Collect commands provided by setup actions in this job
	setupProvidedCommands := j.getSetupProvidedCommands()

	var missingCommands []string
	seen := make(map[string]bool)

	for _, a := range j.stepAnalyses() {
		commands := a.commands
		if a.parseFailed {
			commands = nil
			for _, cmd := range extractCommands(a.raw) {
				commands = append(commands, normalizeCommand(cmd))
			}
		}

		for _, cmdName := range commands {
			if cmdName == "" {
				continue
			}

			// Skip if command is provided by a setup action
			if setupProvidedCommands[cmdName] {
				continue
			}

			// Check if command is missing in slim and not already added
			if IsMissingInSlim(cmdName) && !seen[cmdName] {
				missingCommands = append(missingCommands, cmdName)
				seen[cmdName] = true
			}
		}
	}

	return missingCommands
}

// getSetupProvidedCommands returns a map of commands that are provided by setup actions
// in this job. The map keys are command names, and values are always true.
func (j *Job) getSetupProvidedCommands() map[string]bool {
	providedCommands := make(map[string]bool)

	for _, step := range j.Steps {
		if step.Uses == "" {
			continue
		}

		// Check if this step uses a setup action
		// Setup actions typically follow the pattern: actions/setup-<lang>@<version>
		// We match the base action name without version
		for actionPrefix, commands := range setupActionCommands {
			if strings.HasPrefix(step.Uses, actionPrefix) {
				// This setup action provides these commands
				for _, cmd := range commands {
					providedCommands[cmd] = true
				}
			}
		}
	}

	return providedCommands
}

// extractCommands is the parse-failure fallback: it extracts command names
// from a script line by line, splitting on common shell operators. It handles
// comments, variable assignments, and prefix commands, but not heredocs or
// substitutions — prefer the syntax-tree analysis in analyzeScript.
func extractCommands(script string) []string {
	var commands []string
	lines := strings.Split(script, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Skip comment lines
		if strings.HasPrefix(line, "#") {
			continue
		}

		// Extract commands before pipe, redirect, or logical operators
		parts := splitCommandLine(line)
		for _, part := range parts {
			cmd := extractCommandFromPart(part)
			if cmd != "" {
				commands = append(commands, cmd)
			}
		}
	}

	return commands
}

// splitCommandLine splits a command line by pipe, redirect, and logical operators
// while preserving the command parts.
func splitCommandLine(line string) []string {
	// Split by |, &&, ||, ;, >, <, >>, <<
	// Simple approach: split by these operators
	parts := []string{line}
	separators := []string{"|", "&&", "||", ";", ">>", "<<", ">", "<"}

	for _, sep := range separators {
		var newParts []string
		for _, part := range parts {
			split := strings.Split(part, sep)
			for _, s := range split {
				s = strings.TrimSpace(s)
				if s != "" {
					newParts = append(newParts, s)
				}
			}
		}
		parts = newParts
	}

	return parts
}

// extractCommandFromPart extracts the command name from a command part.
// It handles prefixes like sudo, env, time, etc.
func extractCommandFromPart(part string) string {
	part = strings.TrimSpace(part)
	if part == "" {
		return ""
	}

	// Handle variable assignments (VAR=value command)
	// Split by space first to handle cases like "VAR=value command"
	fields := strings.Fields(part)
	if len(fields) == 0 {
		return ""
	}

	// Find the first field that doesn't contain = (the actual command)
	startIndex := 0
	for startIndex < len(fields) {
		if !strings.Contains(fields[startIndex], "=") {
			break
		}
		startIndex++
	}

	if startIndex >= len(fields) {
		// All fields contain =, no command found
		return ""
	}

	fields = fields[startIndex:]

	// Skip wrapper prefixes such as sudo and env
	cmdStartIndex := 0
	for cmdStartIndex < len(fields) && wrapperCommands[fields[cmdStartIndex]] {
		cmdStartIndex++
	}

	if cmdStartIndex >= len(fields) {
		return ""
	}

	return fields[cmdStartIndex]
}

// normalizeCommand normalizes a command name by removing path components.
// It returns only the basename of the command.
func normalizeCommand(cmd string) string {
	if cmd == "" {
		return ""
	}

	// Remove path components
	if strings.Contains(cmd, "/") {
		parts := strings.Split(cmd, "/")
		cmd = parts[len(parts)-1]
	}

	// Remove common suffixes that might be part of the command
	cmd = strings.TrimSpace(cmd)
	return cmd
}
