package go_language

import (
	"cmp"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/system-inc/cohere/internal/go_language/cache"
	"github.com/system-inc/cohere/internal/go_language/check"
	"github.com/system-inc/cohere/internal/go_language/load"
	"github.com/system-inc/cohere/internal/go_language/project"
	"github.com/system-inc/cohere/internal/go_language/rule"
)

// standardLibrarySource is where a run's standard-library entries come from and go, the zero value a plain run's:
// the system cache, its entries keyed by the executable's identity (see systemCache).
type standardLibrarySource struct {
	// stamp, when not empty, stands for the executable's identity in every standard-library key, for a test that fills
	// a system cache under an identity of its own; every run leaves it empty.
	stamp string

	// toolchain, when not empty, stands for go env's toolchain in the standard library's dependence records' keys,
	// for a test that fakes a toolchain the records were written under (#6a3kjvz); every run leaves it empty.
	toolchain string

	// systemCacheRoot, when not empty, is the system cache's root in place of cache.SystemCacheRoot's, for a test that
	// gives a run a system cache of its own beside others running in parallel; every run leaves it empty.
	systemCacheRoot string

	// goAnswers, when not nil, are go env's and `go tool dist list`'s answers the test process keeps, which the run's
	// go commands read in place of starting a child where one was kept under the same key (load.GoAnswers), for a
	// test process running hundreds of runs, each fork of which faults its heap (#2htryg8); every run leaves it nil.
	goAnswers *load.GoAnswers
}

// errorOverlaid is why a build whose go flags name an overlay keeps no system cache (overlaidGoFlags), which the run
// says.
var errorOverlaid = errors.New("the go flags name an overlay (-overlay), whose files no system cache key holds")

// lintCache is the per-package findings cache the lint phase reads and writes, at the root's `.cache/cohere/go`, beside
// the front door's own cache in `.cache/cohere`, with the standard library's entries in the system cache's store,
// system, when the run keeps one (#syscache). Under `--no-cache` it reads and writes no store: it is nil, or, when the
// run keeps a system cache, a Caching with the system cache alone, since the standard library's entries are of cohere
// and the Go installation and nothing of the project's tree. Under `--no-system-cache` the standard library is keyed
// and stored as any other package, as it is when the run keeps no system cache. A run judging each build's dependence
// reads the system cache as any other run does: the standard library it restores is judged on a graph of its own
// (buildDependenceGraph, #ss4t5g6). It is nil with an error when what every key shares cannot be read, which the run
// says on stderr and runs without it. environment is what the load the cache serves runs under, the workspace's own for
// the host's build and with an extra build's target after it for that build, and goEnvironment is go env's answer the
// load reads there, so the build context every key shares is the one of the build the load is of, read from the go env
// the load asked rather than from one of its own (#4rpt1jh), and so each build reads its own context's standard-library
// entries. A build whose go flags name an overlay keeps no system cache, and says why (overlaidGoFlags). goEnvironment
// is nil only for a host whose census judged no file, which asked go env nothing; then it is read here, recorded in
// children, the run's account of the go commands it starts (see load.Children). The project's store holds stamps, the
// run's shard stamps, as the system cache's does, so a write by any session of the run marks the shard it changes moved
// for every build's pack (cache.Open).
func lintCache(
	request invocation,
	workspace project.Workspace,
	environment []string,
	goEnvironment *load.GoEnvironment,
	enabled []rule.Rule,
	children *load.Children,
	system *systemCache,
	stamps *cache.ShardStamps,
) (*check.Caching, error) {
	source := request.standardLibrary
	noCache := request.switches["no-cache"]
	if noCache && system.store == nil {
		return nil, nil
	}
	answered := goEnvironment
	if answered == nil {
		var environmentError error
		answered, environmentError = load.ReadGoEnvironment(workspace.Root, environment, nil, children)
		if environmentError != nil {
			return nil, environmentError
		}
	}
	values := answered.Values()
	// A build whose go flags name an overlay keeps no system cache, as a run without one keeps none (overlaidGoFlags).
	keepsSystem := system.store != nil
	if keepsSystem && overlaidGoFlags(values["GOFLAGS"]) {
		keepsSystem = false
		system.notKeptBecause(cache.NotKeptBecause(errorOverlaid))
	}
	if noCache && !keepsSystem {
		return nil, nil
	}
	identity, identityError := cache.ExecutableIdentity()
	if identityError != nil {
		return nil, fmt.Errorf("identifying cohere-go: %w", identityError)
	}
	var standardLibrary *check.StandardLibraryCaching
	// The standard library is keyed apart when there is a system cache to keep it.
	if keepsSystem {
		standardLibrary = &check.StandardLibraryCaching{
			System: system.store,
			Context: cache.StandardLibraryContext{
				FormatVersion:      check.CacheFormatVersion,
				ExecutableIdentity: cmp.Or(source.stamp, identity),
				Build:              standardLibraryBuild(values),
				CgoToolchain:       cgoToolchain(values),
				Goroot:             values["GOROOT"],
			},
			// A standard-library dependence record is written under the toolchain that loaded it (#6a3kjvz).
			Toolchain: cmp.Or(source.toolchain, values["GOVERSION"]+" "+values["GOROOT"]),
		}
	}
	if noCache {
		return &check.Caching{StandardLibrary: standardLibrary, CgoToolchain: cgoToolchain(values)}, nil
	}
	return &check.Caching{
		Store:       cache.Open(workspace.Root, stamps),
		ExtraInputs: newStaticcheckConfigurationInputs().inputs,
		Context: cache.Context{
			FormatVersion:      check.CacheFormatVersion,
			ExecutableIdentity: identity,
			Build:              cacheBuild(values),
			RuleSet:            cacheRuleSet(enabled),
		},
		StandardLibrary: standardLibrary,
		CgoToolchain:    cgoToolchain(values),
		AnswersInPart:   request.answersInPart,
	}, nil
}

// overlaidGoFlags reports whether goFlags, `GOFLAGS` as go env reports it, from the environment or go's own env file,
// names an overlay: go list then reads each file the overlay replaces from the overlay, and a key of the system cache
// holds the bytes on disk, never the overlay's, so a build under one would keep and restore analyses of bytes it does
// not have (#57wgzhp). go takes an overlay of a file below `GOROOT`, so every build under an overlay keeps no system
// cache, failing closed, whichever files it names. `GOFLAGS` is the one road: go reads no other flags of cohere-go's
// go commands from outside, and cohere-go's own overlays, the rename's proofs, load with no findings cache at all
// (loadRequest.withOverlay). An -overlay with no file names none.
func overlaidGoFlags(goFlags string) bool {
	for _, field := range strings.Fields(goFlags) {
		name, value, _ := strings.Cut(strings.TrimLeft(field, "-"), "=")
		if name == "overlay" && value != "" {
			return true
		}
	}
	return false
}

// cgoToolchain is the C toolchain of values, go env's answer as cacheBuild reads it, each of load.CgoToolchainNames
// as "NAME=value" in its order, which every key holds under the cgo-translated loader (check.Caching.CgoToolchain,
// cache.StandardLibraryContext.CgoToolchain).
func cgoToolchain(values map[string]string) []string {
	toolchain := make([]string, 0, len(load.CgoToolchainNames))
	for _, name := range load.CgoToolchainNames {
		toolchain = append(toolchain, name+"="+values[name])
	}
	return toolchain
}

// processGodebug is the process variable both the project's keys and the standard library's hold
// (standardLibraryBuild), since the check of a package nothing reports on reads it as much as one's that is.
const processGodebug = "GODEBUG"

// processEnvironment are cohere-go process's own variables an analyzer reads, each named in the build context
// whether it is set or not. asmdecl and framepointer read the target from go/build's build.Default, which comes from
// the process's `GOOS` and `GOARCH`, not from go env, so the two can differ. staticcheck's TOML parser, which reads
// staticcheck.conf, reads `BURNTSUSHI_TOML_110`. cohere-go's own `GODEBUG` can change what go/types makes of a
// package, as gotypesalias did until go1.27 removed it (export_data/predeclared.go), so every key holds it, the
// standard library's too (processGodebug): no go/types setting is honored now, and holding it costs only a miss.
var processEnvironment = []string{"GOOS", "GOARCH", "BURNTSUSHI_TOML_110", processGodebug}

// buildNames are what go env reports that every key's build context holds, in its order, each among what
// load.GoEnvironment asks go env (a test holds it to that).
var buildNames = []string{"GOOS", "GOARCH", "CGO_ENABLED", "GOFLAGS", "GOVERSION", "GOEXPERIMENT"}

// cacheBuild is the build context every key shares, in a fixed order: of values, go env's answer under the
// environment as given (load.GoEnvironment.Values), its `GOFLAGS` without the -pgo=off a load adds, the target, cgo,
// the flags with their build tags, the toolchain and its experiments, then the process's own variables an analyzer
// reads. The cache session adds which loader ran, which only the load knows: a project with cgo of its own has every
// package with cgo files type-checked from the cgo tool's outputs, other files than the load that translates nothing
// gives them.
func cacheBuild(values map[string]string) []string {
	build := make([]string, 0, len(buildNames)+len(processEnvironment))
	for _, name := range buildNames {
		if name == "GOFLAGS" {
			build = append(build, name+"="+load.VerdictFlags(values[name]))
			continue
		}
		build = append(build, name+"="+values[name])
	}
	for _, name := range processEnvironment {
		build = append(build, processVariable(name))
	}
	return build
}

// processVariable is cohere-go process's own variable name as a build context names it, whether it is set or not.
func processVariable(name string) string {
	value, set := os.LookupEnv(name)
	if !set {
		return "process " + name + " unset"
	}
	return "process " + name + "=" + value
}

// flagsNoStandardLibraryReads are the go flags that, beyond those load.VerdictFlags leaves out, change nothing of a
// standard-library package's entry, which standardLibraryBuild leaves out of the standard library's key. The standard
// library is in no module, so -mod, -modfile and -modcacherw, which say how go resolves and keeps the main module's
// dependencies, resolve none of it: go finds every standard-library package below `GOROOT` whatever they say. -trimpath
// rewrites the paths go compiles into what it builds, never the paths go list names, and the source loader reads the
// standard library's files by the paths go list names, with it or without it; the key names each file below `GOROOT`
// besides. -buildvcs decides whether go stamps version control into a main package of the main module, which can refuse
// that package's build where go cannot read the tree's version control, and no standard-library package is one, so it
// changes nothing of one either way; with it in the key, a run under `GOFLAGS=-buildvcs=false`, as every cohere
// worktree and cohere-dev set, would key the standard library apart from a run without it (#30kzgj1). cohere-go's own
// key keeps it (load.VerdictFlags). Every other flag stays, as in cohere-go's key (#s5h4q2t): -tags and -race choose
// which files a package holds, and -n can decide whether a load succeeds at all.
var flagsNoStandardLibraryReads = []string{"trimpath", "mod", "modfile", "modcacherw", "buildvcs"}

// standardLibraryBuild is the build context every standard-library key shares, in cacheBuild's order: of values,
// go env's answer as cacheBuild reads it, the target, cgo, the flags narrowed for the standard library (see
// flagsNoStandardLibraryReads), the toolchain and its experiments, then the process's `GODEBUG` (processGodebug). It
// holds none of the process's other variables, since nothing on the fact path reads them: asmdecl and framepointer,
// which read the process's target, have no facts, and no fact analyzer reads staticcheck's config, whose reading
// `BURNTSUSHI_TOML_110` changes.
func standardLibraryBuild(values map[string]string) []string {
	build := make([]string, 0, len(buildNames)+1)
	for _, name := range buildNames {
		if name != "GOFLAGS" {
			build = append(build, name+"="+values[name])
			continue
		}
		kept := []string{}
		for _, field := range strings.Fields(load.VerdictFlags(values[name])) {
			flagName, _, _ := strings.Cut(strings.TrimLeft(field, "-"), "=")
			if !slices.Contains(flagsNoStandardLibraryReads, flagName) {
				kept = append(kept, field)
			}
		}
		build = append(build, name+"="+strings.Join(kept, " "))
	}
	return append(build, processVariable(processGodebug))
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
