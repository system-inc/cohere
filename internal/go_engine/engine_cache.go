package go_engine

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"

	"github.com/system-inc/cohere/internal/go_engine/cache"
	"github.com/system-inc/cohere/internal/go_engine/check"
	"github.com/system-inc/cohere/internal/go_engine/project"
	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// lintCache is the per-package findings cache the lint phase reads and writes, at the root's `.cache/cohere/go`,
// beside the front door's own cache in `.cache/cohere`. It is nil under `--no-cache`, which reads and writes none,
// and with an error when what every key shares cannot be read, which the run says on stderr and runs without it.
func lintCache(request invocation, workspace project.Workspace, enabled []rule.Rule) (*check.Caching, error) {
	if request.switches["no-cache"] {
		return nil, nil
	}
	identity, identityError := cache.ExecutableIdentity()
	if identityError != nil {
		return nil, fmt.Errorf("identifying the engine: %w", identityError)
	}
	build, buildError := cacheBuild(workspace.Root, workspace.Environment())
	if buildError != nil {
		return nil, buildError
	}
	return &check.Caching{
		Store:       cache.Open(workspace.Root),
		ExtraInputs: newStaticcheckConfigurationInputs().inputs,
		Context: cache.Context{
			FormatVersion:  check.CacheFormatVersion,
			EngineIdentity: identity,
			Build:          build,
			RuleSet:        cacheRuleSet(enabled),
		},
	}, nil
}

// processEnvironment are the engine process's own variables an analyzer reads, each named in the build context
// whether it is set or not. asmdecl and framepointer read the target from go/build's build.Default, which comes from
// the process's `GOOS` and `GOARCH`, not from go env, so the two can differ. staticcheck's TOML parser, which reads
// staticcheck.conf, reads `BURNTSUSHI_TOML_110`.
var processEnvironment = []string{"GOOS", "GOARCH", "BURNTSUSHI_TOML_110"}

// cacheBuild is the build context every key shares, in a fixed order: what go env reports in root for the target,
// cgo, the flags with their build tags, the toolchain and its experiments, then the process's own variables an
// analyzer reads. The cache session adds which loader ran, which only the load knows: a project with cgo of its own
// has every package with cgo files type-checked from the cgo tool's outputs, other files than the load that
// translates nothing gives them.
func cacheBuild(root string, environment []string) ([]string, error) {
	names := []string{"GOOS", "GOARCH", "CGO_ENABLED", "GOFLAGS", "GOVERSION", "GOEXPERIMENT"}
	command := exec.Command("go", append([]string{"env", "-json"}, names...)...)
	command.Dir = root
	command.Env = append(os.Environ(), environment...)
	var standardError bytes.Buffer
	command.Stderr = &standardError
	output, runError := command.Output()
	if runError != nil {
		return nil, fmt.Errorf("reading the build context with go env in %s: %w: %s", root, runError,
			strings.TrimSpace(standardError.String()))
	}
	values := map[string]string{}
	if decodeError := json.Unmarshal(output, &values); decodeError != nil {
		return nil, fmt.Errorf("reading the build context with go env in %s: %w", root, decodeError)
	}
	build := make([]string, 0, len(names)+len(processEnvironment))
	for _, name := range names {
		if name == "GOFLAGS" {
			build = append(build, name+"="+verdictFlags(values[name]))
			continue
		}
		build = append(build, name+"="+values[name])
	}
	for _, name := range processEnvironment {
		value, set := os.LookupEnv(name)
		if !set {
			build = append(build, "process "+name+" unset")
			continue
		}
		build = append(build, "process "+name+"="+value)
	}
	return build, nil
}

// flagsNoVerdictReads are the go flags a run's verdict cannot depend on, which verdictFlags leaves out of the key:
// -p bounds how many packages go builds at once, and -v, -x and -work only change what go prints or keeps of its
// work, while every go command runs as it would without them. The list errs narrow on purpose: a flag left in the key
// costs a miss when it changes, a flag left out wrongly replays a verdict another build would not give, so a flag
// joins it only when neither the load's success nor anything a rule reads can see it. That keeps -n in the key, which
// makes go print its commands without running them, so a load that needs cgo's output gets none, and -buildvcs,
// whose true can refuse a main package in a tree whose version control go cannot read. cohere-dev's pool sets -p to
// each token's share, so with it in the key a run under another share missed every package.
var flagsNoVerdictReads = []string{"p", "v", "x", "work"}

// verdictFlags is goFlags, `GOFLAGS` as go env reports it, without the flags in flagsNoVerdictReads, every other
// flag kept as written and in its order. go reads `GOFLAGS` as fields split on white space, each `-name` or
// `-name=value`, one dash or two.
func verdictFlags(goFlags string) string {
	kept := []string{}
	for _, field := range strings.Fields(goFlags) {
		name, _, _ := strings.Cut(strings.TrimLeft(field, "-"), "=")
		if !slices.Contains(flagsNoVerdictReads, name) {
			kept = append(kept, field)
		}
	}
	return strings.Join(kept, " ")
}

// cacheRuleSet is each enabled rule as its name, then its analyzer's options as they were resolved from the
// settings, every flag with its value, as one JSON object, sorted. The flags are read rather than the settings,
// since a flag's default is what an option left out resolves to.
func cacheRuleSet(enabled []rule.Rule) []string {
	ruleSet := make([]string, 0, len(enabled))
	for _, enabledRule := range enabled {
		options := map[string]string{}
		enabledRule.Analyzer.Flags.VisitAll(func(option *flag.Flag) {
			options[option.Name] = option.Value.String()
		})
		if len(options) == 0 {
			ruleSet = append(ruleSet, enabledRule.Name)
			continue
		}
		// Ignored: a map of strings always encodes, and its keys come out sorted.
		encoded, _ := json.Marshal(options)
		ruleSet = append(ruleSet, enabledRule.Name+" "+string(encoded))
	}
	slices.Sort(ruleSet)
	return ruleSet
}
