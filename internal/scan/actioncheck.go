package scan

import (
	"errors"
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/fchimpan/gh-slimify/internal/api"
	"github.com/fchimpan/gh-slimify/internal/workflow"
)

// actionResolver returns the metadata for a uses: reference. References
// starting with "./" are local to the scanned repository; anything else is a
// remote owner/repo[/path]@ref.
type actionResolver func(ref string) (*api.ActionMetadata, error)

// errOfflineResolution marks remote lookups skipped in offline mode.
var errOfflineResolution = errors.New("remote action metadata lookup skipped (offline)")

// maxActionDepth bounds recursion through nested composite actions.
const maxActionDepth = 3

// knownNonDockerPrefixes lists action owners whose actions never run in
// Docker containers; skipping them avoids API calls for the most common
// actions (checkout, setup-*, cache, ...).
var knownNonDockerPrefixes = []string{"actions/"}

// actionVerifier classifies uses: references as Docker container actions by
// reading their action.yml, with a per-scan cache.
type actionVerifier struct {
	resolve actionResolver
	verbose bool
	cache   map[string]bool
}

// verifyContainerActions moves candidates that use Docker container actions
// (runs.using: docker in the action's metadata) into the ineligible list.
// The docker/ prefix heuristic already catches the official Docker actions;
// this pass catches Dockerfile-based actions from any other publisher.
// References that cannot be resolved (offline, rate limit, deleted repo) are
// treated as non-Docker, matching the previous heuristic-only behavior.
func verifyContainerActions(candidates []*Candidate, ineligibleJobs []*IneligibleJob, resolve actionResolver, verbose bool) ([]*Candidate, []*IneligibleJob) {
	v := &actionVerifier{resolve: resolve, verbose: verbose, cache: make(map[string]bool)}

	remaining := candidates[:0]
	for _, c := range candidates {
		var dockerRefs []string
		for _, ref := range c.usesRefs {
			if v.isDockerAction(ref, 0) {
				dockerRefs = append(dockerRefs, ref)
			}
		}
		if len(dockerRefs) > 0 {
			ineligibleJobs = append(ineligibleJobs, c.toIneligible(fmt.Sprintf(
				"uses Docker container action(s) (%s)", strings.Join(dockerRefs, ", "))))
			continue
		}
		remaining = append(remaining, c)
	}
	return remaining, ineligibleJobs
}

// isDockerAction reports whether a uses: reference resolves to an action that
// runs in a Docker container, following composite actions up to maxActionDepth.
func (v *actionVerifier) isDockerAction(ref string, depth int) bool {
	if depth > maxActionDepth {
		return false
	}
	if workflow.IsContainerActionRef(ref) {
		return true
	}
	for _, prefix := range knownNonDockerPrefixes {
		if strings.HasPrefix(ref, prefix) {
			return false
		}
	}

	if cached, ok := v.cache[ref]; ok {
		return cached
	}
	// Pre-seed to break reference cycles between composite actions.
	v.cache[ref] = false

	meta, err := v.resolve(ref)
	if err != nil || meta == nil {
		if v.verbose && err != nil && !errors.Is(err, errOfflineResolution) {
			fmt.Fprintf(os.Stderr, "Warning: could not resolve action metadata for %s: %v\n", ref, err)
		}
		return false
	}

	result := false
	switch strings.ToLower(meta.Using) {
	case "docker":
		result = true
	case "composite":
		for _, step := range meta.Steps {
			if step.Uses != "" && v.isDockerAction(resolveChildRef(ref, step.Uses), depth+1) {
				result = true
				break
			}
			if step.Run != "" && workflow.ScriptUsesContainerTooling(step.Run) {
				result = true
				break
			}
		}
	}

	v.cache[ref] = result
	return result
}

// resolveChildRef resolves a step reference found inside a composite action
// relative to the composite action's own repository.
func resolveChildRef(parent, child string) string {
	if !strings.HasPrefix(child, "./") {
		return child
	}
	rel := strings.TrimPrefix(child, "./")

	if strings.HasPrefix(parent, "./") {
		return "./" + path.Join(strings.TrimPrefix(parent, "./"), rel)
	}

	owner, repo, _, version, err := api.ParseActionRef(parent)
	if err != nil {
		return child
	}
	ref := owner + "/" + repo + "/" + rel
	if version != "" {
		ref += "@" + version
	}
	return ref
}

// newActionResolver resolves local references from disk and remote references
// via the GitHub contents API. A nil client (offline mode) makes remote
// lookups return errOfflineResolution, so detection falls back to the prefix
// heuristics alone.
func newActionResolver(client *api.Client) actionResolver {
	return func(ref string) (*api.ActionMetadata, error) {
		if strings.HasPrefix(ref, "./") {
			return loadLocalActionMetadata(strings.TrimPrefix(ref, "./"))
		}
		if client == nil {
			return nil, errOfflineResolution
		}

		owner, repo, subpath, version, err := api.ParseActionRef(ref)
		if err != nil {
			return nil, err
		}
		return client.GetActionMetadata(owner, repo, subpath, version)
	}
}

// loadLocalActionMetadata reads the metadata of an action that lives in the
// scanned repository (uses: ./path/to/action).
func loadLocalActionMetadata(dir string) (*api.ActionMetadata, error) {
	var lastErr error
	for _, name := range api.ActionMetadataFilenames {
		data, err := os.ReadFile(path.Join(dir, name))
		if err != nil {
			lastErr = err
			continue
		}
		return api.ParseActionMetadata(data)
	}
	return nil, fmt.Errorf("failed to read local action metadata in %s: %w", dir, lastErr)
}
