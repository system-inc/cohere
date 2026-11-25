package go_language

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// A package's variable initializers are one synthetic function, ordered by `types.Info.InitOrder`, and cut after a
// call that never returns, so an initializer's finding can turn on a body or a name of the package the initializer
// never uses (#bed0mnk, the Square's musts 1 and 2). Each case writes a module into the body-reach-initializer-causes
// fixture, whose host-only files are built without the audit_b tag and whose build-only files under it, and wants the
// shared initializers reached, Initializers, under the general rule and the constants-only one, the bodies the rule
// must not reach left unreached, and the body differential reading no failure.
//
// The reader's modules: in the stop cases first calls other, shared, which calls a stop the pair declares returning on
// one side and panicking on the other, so the initializer is cut after first on one side alone, and parsed's SA1007
// with it. In the order cases other never returns in any build and calls a helper the pair declares empty on the host
// and reading parsed under the tag: there parsed is initialized before first and reports. The partial cases hold a
// function more that nothing reaches. The blank case's stopping initializer is a blank one in the host's file alone,
// beside a function, an init function and a const no initializer rule may reach.
//
// Then one case for each cause alone: a shared body reached by another package's difference, a body in a file the
// host alone compiles, a body in a file the build alone compiles, a method's, which declares no package-level name,
// and a type, which has no body, declared by a file of the build's alone and by one of the host's alone.
func TestTheReachReachesEveryInitializerWhereABodyOrANameOfItsPackageDiffers(test *testing.T) {
	test.Parallel()

	// hostOnly and buildOnly open a file one side alone compiles.
	const hostOnly = "//go:build !audit_b\n\npackage audit\n"
	const buildOnly = "//go:build audit_b\n\npackage audit\n"
	const returns = "\nfunc stop() { println(\"returns\") }\n"
	const stops = "\nfunc stop() { panic(\"stops\") }\n"
	// stop is the stop cases' shared file, and partialStop the same with two functions more. Each shared file is
	// written without the line it opens on here.
	const stop = `
// Package audit holds two variable initializers every build shares. first calls other, which this file declares the
// same in every build, and other calls stop, which the pair declares returning on one side and panicking on the
// other. parsed is a call staticcheck reports, SA1007.
package audit

import "net/url"

// first calls other, which stops where stop does.
var first = other()

// parsed is a URL that never parses.
var parsed, _ = url.Parse(":bad")

// other returns where stop does, and stops where it stops.
func other() int {
	stop()
	return 0
}
`
	const partialStop = stop + `
// quiet parses a URL that never parses, in a body nothing reaches.
func quiet() {
	_, _ = url.Parse(":quiet")
}

// caller calls other, whose no-return fact differs.
func caller() int {
	return other()
}
`
	// order is the order cases' shared file, and partialOrder the same with one function more.
	const order = `
// Package audit holds two variable initializers every build shares. first calls other, which never returns in any
// build, so every initializer after first in the initialization order is cut. other calls helper, which the pair
// declares empty on the host and reading parsed under the audit_b tag: there first depends on parsed, so parsed is
// initialized before first, and its SA1007 is the tagged build's alone. No no-return fact differs.
package audit

import "net/url"

// first calls other, which never returns.
var first = other()

// parsed is a URL that never parses.
var parsed, _ = url.Parse(":bad")

// other never returns, in any build.
func other() int {
	helper()
	panic("always")
}
`
	const partialOrder = order + `
// quiet parses a URL that never parses, in a body nothing reaches.
func quiet() {
	_, _ = url.Parse(":quiet")
}
`
	// blank is the blank case's shared file: the reader's, with an init function and a const more.
	const blank = `
// Package audit holds variable initializers every build shares, in the shapes a value spec takes: a grouped
// declaration, two names from one call, and blank names alone. The stopping initializer is a blank one, in a file
// one build alone compiles, sorted before this file.
package audit

import (
	"net/url"
	"regexp"
	"time"
)

var (
	// parsed is a URL that never parses.
	parsed, _ = url.Parse(":bad")

	// pattern is an expression that never compiles.
	pattern = regexp.MustCompile("(")
)

// A blank spec, whose layout holds no month 13.
var _, _ = time.Parse("2006-13-01", "x")

// limit is a const, which no initializer runs.
const limit = 3

// stop never returns.
func stop() int {
	panic("stop")
}

// init is a function of its own, called after the variables.
func init() {
	println(limit)
}
`
	// crossed is a shared file whose other calls another package's Stop, which that package's pair declares, so
	// other is reached and no file of this package differs.
	const crossed = `
// Package audit holds two variable initializers and a function, in one file every build shares.
package audit

import (
	"net/url"

	"example.com/audit/dep"
)

// first calls other, which stops where dep's Stop does.
var first = other()

// parsed is a URL that never parses.
var parsed, _ = url.Parse(":bad")

// other returns where dep's Stop does, and stops where it stops.
func other() int {
	dep.Stop()
	return 0
}

// quiet parses a URL that never parses, in a body nothing reaches.
func quiet() {
	_, _ = url.Parse(":quiet")
}
`
	// alone is the shared file of the cases that hold one cause alone in a file one side compiles.
	const alone = `
// Package audit holds one variable initializer, a function and a type, in one file every build shares.
package audit

import "net/url"

// parsed is a URL that never parses.
var parsed, _ = url.Parse(":bad")

// held is a type a build-only file may declare a method of.
type held struct{}

// quiet parses a URL that never parses, in a body nothing reaches.
func quiet() {
	_, _ = url.Parse(":quiet")
}
`
	const extra = "\n// extra is a function nothing shared calls.\nfunc extra() int { return 1 }\n"
	const method = "\n// extra is a method nothing shared calls.\nfunc (held) extra() int { return 1 }\n"
	const marker = "\n// marker is a type nothing shared names.\ntype marker struct{}\n"
	for _, each := range []struct {
		name string
		// files are the module's files beside its go.mod, by name.
		files map[string]string
		// reached are the shared file's initializers, each wanted reached by Initializers, and unreached its bodies
		// wanted left unreached.
		reached   []string
		unreached []string
	}{
		{"stopping under the tag", map[string]string{"shared.go": stop, "stop_a.go": hostOnly + returns,
			"stop_b.go": buildOnly + stops}, []string{"first", "parsed"}, nil},
		{"stopping on the host", map[string]string{"shared.go": stop, "stop_a.go": hostOnly + stops,
			"stop_b.go": buildOnly + returns}, []string{"first", "parsed"}, nil},
		{"stopping under the tag, in part", map[string]string{"shared.go": partialStop,
			"stop_a.go": hostOnly + returns, "stop_b.go": buildOnly + stops}, []string{"first", "parsed"},
			[]string{"quiet", "caller"}},
		{"stopping on the host, in part", map[string]string{"shared.go": partialStop, "stop_a.go": hostOnly + stops,
			"stop_b.go": buildOnly + returns}, []string{"first", "parsed"}, []string{"quiet", "caller"}},
		{"ordered otherwise under the tag", map[string]string{"shared.go": order,
			"helper_a.go": hostOnly + "\nfunc helper() {}\n",
			"helper_b.go": buildOnly + "\nfunc helper() { _ = parsed }\n"}, []string{"first", "parsed"}, nil},
		{"ordered otherwise under the tag, in part", map[string]string{"shared.go": partialOrder,
			"helper_a.go": hostOnly + "\nfunc helper() {}\n",
			"helper_b.go": buildOnly + "\nfunc helper() { _ = parsed }\n"}, []string{"first", "parsed"},
			[]string{"quiet"}},
		{"a blank initializer in the host's file alone", map[string]string{"shared.go": blank,
			"early_a.go": hostOnly + "\nvar _ = stop()\n", "other_b.go": buildOnly},
			[]string{"parsed", "pattern", "_"}, []string{"limit", "stop", "init"}},
		{"a shared body reached, alone", map[string]string{"shared.go": crossed,
			"dep/shared.go": "// Package dep holds Stop, which the pair declares.\npackage dep\n",
			"dep/stop_a.go": "//go:build !audit_b\n\npackage dep\n\n" +
				"// Stop returns.\nfunc Stop() { println(\"returns\") }\n",
			"dep/stop_b.go": "//go:build audit_b\n\npackage dep\n\n" +
				"// Stop never returns.\nfunc Stop() { panic(\"stops\") }\n"},
			[]string{"first", "parsed"}, []string{"quiet"}},
		{"a body in the host's file, alone", map[string]string{"shared.go": alone, "part_a.go": hostOnly + extra,
			"part_b.go": buildOnly}, []string{"parsed"}, []string{"quiet"}},
		{"a body in the build's file, alone", map[string]string{"shared.go": alone, "part_a.go": hostOnly,
			"part_b.go": buildOnly + method}, []string{"parsed"}, []string{"quiet"}},
		{"a name in the build's file, alone", map[string]string{"shared.go": alone, "part_a.go": hostOnly,
			"part_b.go": buildOnly + marker}, []string{"parsed"}, []string{"quiet"}},
		{"a name in the host's file, alone", map[string]string{"shared.go": alone, "part_a.go": hostOnly + marker,
			"part_b.go": buildOnly}, []string{"parsed"}, []string{"quiet"}},
	} {
		test.Run(each.name, func(test *testing.T) {
			test.Parallel()
			// The reach and its census judge what each build checks from source, the standard library's packages
			// among them, so every run computes them rather than restore them from the template.
			coldSystemCacheFor(test)

			root := copyFixture(test, filepath.Join("testdata", "body-reach-initializer-causes"))
			for name, contents := range each.files {
				if makeError := os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0o755); makeError != nil {
					test.Fatal(makeError)
				}
				written := []byte(strings.TrimPrefix(contents, "\n"))
				if writeError := os.WriteFile(filepath.Join(root, name), written, 0o644); writeError != nil {
					test.Fatal(writeError)
				}
			}
			on, _ := runOverConfigured(test, root, exitFindings, func(pipeline *pipeline) {
				pipeline.bodyReachCensus = filepath.Join(test.TempDir(), "reach.txt")
				pipeline.bodyDifferential = true
			}, "--no-cache", "--no-fix", "--build-dependence")

			tagged := slices.IndexFunc(on.buildRuns, func(run buildRun) bool {
				return slices.Contains(run.build.Tags, "audit_b")
			})
			if tagged < 0 {
				test.Fatalf("want a build under the audit_b tag, got %d runs", len(on.buildRuns))
			}
			census, differential := on.buildRuns[tagged].bodyReach, on.buildRuns[tagged].bodyDifferential
			if census == nil || differential == nil {
				test.Fatal("the tagged run has no census or no body differential")
			}
			sharedFile := filepath.Join(root, "shared.go")
			reasons, constantsOnly := map[string]string{}, map[string]string{}
			for _, body := range census.bodies {
				if body.packageIdentifier == "example.com/audit" && body.file == sharedFile {
					reasons[body.name] = string(body.reason)
					constantsOnly[body.name] = string(body.constantsOnlyReason)
				}
			}
			for _, name := range each.reached {
				want := string(reachedInitializers)
				if reasons[name] != want || constantsOnly[name] != want {
					test.Errorf("the shared initializer %s is reached by %q, and by %q under the constants-only rule, "+
						"want %s by both, of %v", name, reasons[name], constantsOnly[name], want, reasons)
				}
			}
			for _, name := range each.unreached {
				if reason, isJudged := reasons[name]; !isJudged || reason != "" {
					test.Errorf("the shared body %s is judged %t and reached by %q, want it left unreached, of %v",
						name, isJudged, reason, reasons)
				}
			}
			var failures []string
			for _, failure := range differential.failures {
				failures = append(failures, strings.ReplaceAll(failure, root+string(filepath.Separator), ""))
			}
			if len(failures) > 0 {
				test.Errorf("want the body differential to read no failure, got\n%s", strings.Join(failures, "\n"))
			}
		})
	}
}
