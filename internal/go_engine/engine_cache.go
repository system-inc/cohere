package go_engine

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/system-inc/cohere/internal/go_engine/cache"
	"github.com/system-inc/cohere/internal/go_engine/check"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/project"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/standard_library_table"
)

// builtInStandardLibrary is the standard library's entries a run may read from the binary: the table, and the release
// stamp it was written under, empty in a build that is no release. collected, when it is not nil, gains the
// standard-library entries a cached run saves (see check.StandardLibraryCaching), for a test, or the release, to write
// a table from.
type builtInStandardLibrary struct {
	table     *cache.StandardLibraryTable
	stamp     string
	collected map[cache.Key]check.StandardLibraryRunEntry
}

// embeddedStandardLibrary is the binary's own, parsed on its first lookup.
var embeddedStandardLibrary = builtInStandardLibrary{
	table: cache.NewStandardLibraryTable(standard_library_table.Contents),
	stamp: standard_library_table.ReleaseStamp,
}

// lintCache is the per-package findings cache the lint phase reads and writes, at the root's `.cache/cohere/go`,
// beside the front door's own cache in `.cache/cohere`, with the standard library's entries the binary carries when
// it carries any under a release stamp. An empty stamp reads no table at all, so a build that is no release computes
// the standard library as it always has. Under `--no-cache` it reads and writes no store: it is nil, or, when the
// binary carries a table, a Caching with that table alone, since the standard library's entries are part of cohere
// itself and nothing of the project's tree. Under `--build-dependence` beside `--other-builds` it is nil, the table
// included (judgesDependence). It is nil with an error when what every key shares cannot be read, which
// the run says on stderr and runs without it. environment is what the load the cache serves runs under, the
// workspace's own for the host's build and with an extra build's target after it for that build, and goEnvironment
// is go env's answer the load reads there, so the build context every key shares is the one of the build the load is
// of, read from the go env the load asked rather than from one of its own (#4rpt1jh). goEnvironment is nil only for a
// host whose census judged no file, which asked go env nothing; then it is read here.
func lintCache(
	request invocation,
	workspace project.Workspace,
	environment []string,
	goEnvironment *load.GoEnvironment,
	enabled []rule.Rule,
) (*check.Caching, error) {
	builtIn := request.standardLibrary
	holdsTable := builtIn.table != nil && !builtIn.table.Empty()
	readsBuiltIn := builtIn.stamp != "" && (holdsTable || builtIn.collected != nil)
	noCache := request.switches["no-cache"]
	// A run judging each build's dependence reads no store and no table, as `--no-cache` in a binary with no table
	// does, so the host checks every package from source (judgesDependence).
	if request.judgesDependence() || noCache && (!readsBuiltIn || !holdsTable) {
		return nil, nil
	}
	answered := goEnvironment
	if answered == nil {
		var environmentError error
		answered, environmentError = load.ReadGoEnvironment(workspace.Root, environment, nil)
		if environmentError != nil {
			return nil, environmentError
		}
	}
	values := answered.Values()
	var standardLibrary *check.StandardLibraryCaching
	if readsBuiltIn {
		standardLibrary = &check.StandardLibraryCaching{
			Context: cache.StandardLibraryContext{
				FormatVersion: check.CacheFormatVersion,
				ReleaseStamp:  builtIn.stamp,
				Build:         standardLibraryBuild(values),
				Goroot:        values["GOROOT"],
			},
			Collected: builtIn.collected,
		}
		if holdsTable {
			standardLibrary.Table = builtIn.table
		}
	}
	if noCache {
		return &check.Caching{StandardLibrary: standardLibrary}, nil
	}
	identity, identityError := cache.ExecutableIdentity()
	if identityError != nil {
		return nil, fmt.Errorf("identifying the engine: %w", identityError)
	}
	return &check.Caching{
		Store:       cache.Open(workspace.Root),
		ExtraInputs: newStaticcheckConfigurationInputs().inputs,
		Context: cache.Context{
			FormatVersion:  check.CacheFormatVersion,
			EngineIdentity: identity,
			Build:          cacheBuild(values),
			RuleSet:        cacheRuleSet(enabled),
		},
		StandardLibrary: standardLibrary,
		AnswersInPart:   request.answersInPart,
	}, nil
}

// processEnvironment are the engine process's own variables an analyzer reads, each named in the build context
// whether it is set or not. asmdecl and framepointer read the target from go/build's build.Default, which comes from
// the process's `GOOS` and `GOARCH`, not from go env, so the two can differ. staticcheck's TOML parser, which reads
// staticcheck.conf, reads `BURNTSUSHI_TOML_110`.
var processEnvironment = []string{"GOOS", "GOARCH", "BURNTSUSHI_TOML_110"}

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
		value, set := os.LookupEnv(name)
		if !set {
			build = append(build, "process "+name+" unset")
			continue
		}
		build = append(build, "process "+name+"="+value)
	}
	return build
}

// flagsNoStandardLibraryReads are the go flags that, beyond those load.VerdictFlags leaves out, change nothing of a
// standard-library package's entry, which standardLibraryBuild leaves out of the standard library's key. The standard
// library is in no module, so -mod, -modfile and -modcacherw, which say how go resolves and keeps the main module's
// dependencies, resolve none of it: go finds every standard-library package below `GOROOT` whatever they say. -trimpath
// rewrites the paths go compiles into what it builds, never the paths go list names, and the source loader reads the
// standard library's files by the paths go list names, with it or without it; the key names each file below `GOROOT`
// besides. Every other flag stays, as in the engine's key (#s5h4q2t): -tags and -race choose which files a package
// holds, and -n and -buildvcs can decide whether a load succeeds at all.
var flagsNoStandardLibraryReads = []string{"trimpath", "mod", "modfile", "modcacherw"}

// standardLibraryBuild is the build context every standard-library key shares, in cacheBuild's order: of values,
// go env's answer as cacheBuild reads it, the target, cgo, the flags narrowed for the standard library (see
// flagsNoStandardLibraryReads), the toolchain and its experiments. It holds none of the process's own variables, since
// nothing on the fact path reads them: asmdecl and framepointer, which read the process's target, have no facts, and
// no fact analyzer reads staticcheck's config, whose reading `BURNTSUSHI_TOML_110` changes.
func standardLibraryBuild(values map[string]string) []string {
	build := make([]string, 0, len(buildNames))
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
	return build
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
