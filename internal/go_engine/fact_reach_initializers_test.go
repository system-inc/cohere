package go_engine

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/fact_reach"
	"github.com/system-inc/cohere/internal/go_engine/rules/standing_facts"
)

// A package's variable initializers are one synthetic function, cut after a call that never returns, and a rule reads
// a callee's answer there as in any body. Across packages neither moves with anything the body reach follows: the
// callee's body is another package's, and no body of this one is reached. So the facts' reach holds the initializers
// as one asker (#bed0mnk, the Square's must 1, across packages): where a function any of them uses is named, for a
// no-return fact or an answer, every one leaves, the body differential counts them taken out, and the census's
// "declarations only" list lets the package go. Each case writes a module into the body-reach-initializer-causes
// fixture, whose dep package holds a function every build shares that calls one the pair declares.
//
// The reader's two modules: first calls dep.Other, which stops under the audit_b tag, and with a function of the
// package calling it as a statement, where ctrlflow reads another package's no-return fact, the build's initializer is
// cut after first and parsed's SA1007 is the host's alone; with no such body the fact is never read, both builds
// report, and nothing is named. Then a function named that no initializer uses, which leaves the initializers
// compared, and a function the initializers alone use, named by its purity, an answer the host read of dep.
//
// Then the two shapes with no function declared, where nothing can stand and the package is settled all the same: the
// statement in a function literal an initializer holds, and the answer with the initializer alone. With the standing
// bodies on, the build still lints such a package whole: its findings and the stream's are those with the switch off.
func TestTheFactsReachTakesAPackagesVariableInitializersOutTogether(test *testing.T) {
	test.Parallel()

	const hostOnly = "//go:build !audit_b\n\npackage dep\n"
	const buildOnly = "//go:build audit_b\n\npackage dep\n"
	// stopping is the dep package whose Other stops under the tag, and quieting the one whose Other is pure there.
	stopping := map[string]string{
		"dep/shared.go": "// Package dep holds Other, the same in every build, which calls stop, the pair's.\n" +
			"package dep\n\n// Other returns where stop does, and stops where it stops.\n" +
			"func Other() int {\n\tstop()\n\treturn 0\n}\n",
		"dep/stop_a.go": hostOnly + "\nfunc stop() { println(\"returns\") }\n",
		"dep/stop_b.go": buildOnly + "\nfunc stop() { panic(\"stops\") }\n",
	}
	quieting := map[string]string{
		"dep/shared.go": "// Package dep holds Other, the same in every build, which returns what quiet does.\n" +
			"package dep\n\n// Other is pure where quiet is.\n" +
			"func Other(value int) (int, int) {\n\treturn quiet(value), value\n}\n",
		"dep/quiet_a.go": hostOnly + "\nfunc quiet(value int) int {\n\tprintln(value)\n\treturn value + 1\n}\n",
		"dep/quiet_b.go": buildOnly + "\nfunc quiet(value int) int { return value + 1 }\n",
	}
	// Each shared file is written without the line it opens on here.
	const bare = `
// Package audit holds two variable initializers every build shares. first calls dep.Other, which dep declares the same
// in every build, and which stops under the audit_b tag, where dep's stop does.
package audit

import (
	"net/url"

	"example.com/audit/dep"
)

// first calls dep.Other, which stops where dep's stop does.
var first = dep.Other()

// parsed is a URL that never parses.
var parsed, _ = url.Parse(":bad")
`
	const statement = bare + `
// use calls dep.Other as a statement.
func use() {
	dep.Other()
}
`
	const unused = `
// Package audit holds one variable initializer, which uses nothing of dep, and a function that calls dep.Other as a
// statement.
package audit

import (
	"net/url"

	"example.com/audit/dep"
)

// parsed is a URL that never parses.
var parsed, _ = url.Parse(":bad")

// use calls dep.Other as a statement.
func use() {
	dep.Other()
}
`
	const answered = `
// Package audit holds one variable initializer, which alone uses dep.Other, and a function nothing reaches, so the
// build lints the package in part.
package audit

import "example.com/audit/dep"

// A blank spec, whose call's results are discarded, a finding where dep.Other is pure.
var _, _ = dep.Other(1)

// idle uses nothing of dep.
func idle() int {
	return 1
}
`
	const literal = bare + `
// asker calls dep.Other as a statement, in a function literal.
var asker = func() {
	dep.Other()
}
`
	const alone = `
// Package audit holds one variable initializer and nothing else, which uses dep.Other.
package audit

import "example.com/audit/dep"

// A blank spec, whose call's results are discarded, a finding where dep.Other is pure.
var _, _ = dep.Other(1)
`
	for _, each := range []struct {
		name string
		// dep is the dep package's files and shared the audit package's one file.
		dep    map[string]string
		shared string
		// initializers are the shared file's initializers, none of which may stand, and takenOut how many bodies the
		// differential counts taken out by the facts' reach.
		initializers []string
		takenOut     int
		// stands is how many functions of the package stand as the walk starts. The build settles the package with
		// none standing too, its initializers being unreached, and then still lints it whole. declarationsOnly is
		// whether the census file lists the package under "declarations only".
		stands           int
		declarationsOnly bool
		// rule is the finding of an initializer the builds may differ on, hostReports and buildReports whether each
		// reports it.
		rule         string
		hostReports  bool
		buildReports bool
	}{
		{"a statement in a function of the package", stopping, statement, []string{"first", "parsed"}, 3, 1, false,
			"cohere-go/SA1007", true, false},
		{"no asking body", stopping, bare, []string{"first", "parsed"}, 0, 0, true, "cohere-go/SA1007", true, true},
		{"a named function no initializer uses", stopping, unused, []string{"parsed"}, 1, 1, false,
			"cohere-go/SA1007", true, true},
		{"an answer about a function the initializers alone use", quieting, answered, []string{"_"}, 1, 1, false,
			"cohere-go/SA4017", false, true},
		{"a statement in a function literal, no function declared", stopping, literal,
			[]string{"first", "parsed", "asker"}, 3, 0, false, "cohere-go/SA1007", true, false},
		{"an answer, no function declared", quieting, alone, []string{"_"}, 1, 0, false, "cohere-go/SA4017", false,
			true},
	} {
		test.Run(each.name, func(test *testing.T) {
			test.Parallel()

			files := map[string]string{"shared.go": strings.TrimPrefix(each.shared, "\n")}
			for name, contents := range each.dep {
				files[name] = contents
			}
			// written is a fresh copy of the fixture holding the case's module.
			written := func() string {
				root := copyFixture(test, filepath.Join("testdata", "body-reach-initializer-causes"))
				for name, contents := range files {
					if makeError := os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0o755); makeError != nil {
						test.Fatal(makeError)
					}
					writeError := os.WriteFile(filepath.Join(root, name), []byte(contents), 0o644)
					if writeError != nil {
						test.Fatal(writeError)
					}
				}
				return root
			}
			root := written()
			censusFile := filepath.Join(test.TempDir(), "reach.txt")
			on, _ := runOverConfigured(test, root, exitFindings, func(engine *pipeline) {
				engine.bodyReachCensus = censusFile
				engine.bodyDifferential = true
			}, "--no-cache", "--no-fix", "--build-dependence")

			tagged := slices.IndexFunc(on.buildRuns, func(run buildRun) bool {
				return slices.Contains(run.build.Tags, "audit_b")
			})
			if tagged < 0 {
				test.Fatalf("want a build under the audit_b tag, got %d runs", len(on.buildRuns))
			}
			run := on.buildRuns[tagged]
			if run.bodyReach == nil || run.bodyDifferential == nil {
				test.Fatal("the tagged run has no census or no body differential")
			}
			var failures []string
			for _, failure := range run.bodyDifferential.failures {
				failures = append(failures, strings.ReplaceAll(failure, root+string(filepath.Separator), ""))
			}
			if len(failures) > 0 {
				test.Errorf("want the body differential to read no failure, got\n%s", strings.Join(failures, "\n"))
			}
			if run.bodyDifferential.takenOut != each.takenOut {
				test.Errorf("the body differential counts %d bodies taken out by the facts' reach, want %d",
					run.bodyDifferential.takenOut, each.takenOut)
			}

			// The finding the case turns on, by who reports it: where the builds differ, the initializers had to leave.
			sharedFile := filepath.Join(root, "shared.go")
			reports := func(reporting buildRun) bool {
				for _, finding := range reporting.result.Findings {
					if finding.Rule == each.rule && finding.File == sharedFile {
						return true
					}
				}
				return false
			}
			if reports(on.buildRuns[0]) != each.hostReports || reports(run) != each.buildReports {
				test.Errorf("%s is reported by the host %t and by the tagged build %t, want %t and %t", each.rule,
					reports(on.buildRuns[0]), reports(run), each.hostReports, each.buildReports)
			}

			// No initializer is a place of the side's, so none stands as the walk starts or after it. The build settles
			// the package all the same, with every function standing or with none, and its counts are filled, which the
			// census sums: dep.Other's no-return fact is compared wherever anything of the package uses it.
			hosted := run.factHosts["example.com/audit"]
			if hosted == nil {
				test.Fatal("the tagged build handed its passes no side of the package")
			}
			standing, isSettled := hosted.Standing()
			if !isSettled || len(standing) != each.stands {
				test.Errorf("the build settles the package %t with %d bodies standing, want it settled with %d",
					isSettled, len(standing), each.stands)
			}
			settled, _ := run.result.FactReachSettled["example.com/audit"].(*standing_facts.Result)
			if settled == nil || !settled.Partial || settled.Settlement == nil || settled.FunctionsCompared == 0 {
				test.Fatalf("the package's settled result is %+v, want one settled, its functions compared", settled)
			}
			if run.factReach == nil || run.factReach.settled == 0 || run.factReach.functionsCompared == 0 {
				test.Errorf("the build's census of the facts' reach is %+v, want the package among those settled",
					run.factReach)
			}
			for _, body := range run.bodyReach.bodies {
				if body.packageIdentifier != "example.com/audit" || !slices.Contains(each.initializers, body.name) {
					continue
				}
				place := fact_reach.Place{File: body.file, Offset: body.offset}
				if body.reason != "" || standing[place] || slices.Contains(hosted.Places(), place) ||
					settled.Stands(body.file, body.offset) {
					test.Errorf("the initializer %s is reached by %q, stands as the walk starts %t, is a place of the "+
						"side's %t and stands settled %t, want it unreached and none of the rest", body.name,
						body.reason, standing[place], slices.Contains(hosted.Places(), place),
						settled.Stands(body.file, body.offset))
				}
			}

			// The census file's "declarations only" list, the lines under its count.
			census, readError := os.ReadFile(censusFile)
			if readError != nil {
				test.Fatal(readError)
			}
			isListed, inList := false, false
			for line := range strings.Lines(string(census)) {
				switch {
				case strings.HasPrefix(line, "  declarations only: "):
					inList = true
				case !strings.HasPrefix(line, "    "):
					inList = false
				case inList && strings.TrimSpace(line) == "example.com/audit":
					isListed = true
				}
			}
			if isListed != each.declarationsOnly {
				test.Errorf("the census file lists the package under \"declarations only\" %t, want %t:\n%s", isListed,
					each.declarationsOnly, census)
			}

			// A package settled with nothing standing is still linted whole with the standing bodies on: the tagged
			// build's own findings and the stream's are those with the switch off, none missing and none added.
			if each.stands > 0 {
				return
			}
			var found [2][]string
			var streams [2][]string
			for index, switched := range []bool{false, true} {
				standingRoot := written()
				switchedRun, stream := runOverConfigured(test, standingRoot, exitFindings, func(engine *pipeline) {
					engine.bodyReachCensus = filepath.Join(test.TempDir(), "reach.txt")
					engine.standingBodies = switched
				}, "--no-cache", "--no-fix", "--build-dependence")
				streams[index] = findingLines(normalize(stream, standingRoot))
				at := slices.IndexFunc(switchedRun.buildRuns, func(run buildRun) bool {
					return slices.Contains(run.build.Tags, "audit_b")
				})
				if at < 0 {
					test.Fatalf("with the standing bodies %t, want a build under the audit_b tag", switched)
				}
				found[index] = rawFindings(switchedRun.buildRuns[at], standingRoot)
				if switchedRun.buildRuns[at].standingDeclarations != 0 {
					test.Errorf("with the standing bodies %t the tagged build shows the host's findings in %d "+
						"declarations, want none", switched, switchedRun.buildRuns[at].standingDeclarations)
				}
			}
			if len(found[0]) == 0 || !slices.Equal(found[0], found[1]) || !slices.Equal(streams[0], streams[1]) {
				test.Errorf("with the standing bodies on the tagged build's findings are\n%s\nand off\n%s\nand the "+
					"streams' findings alike %t, want the same findings, and some", strings.Join(found[1], "\n"),
					strings.Join(found[0], "\n"), slices.Equal(streams[0], streams[1]))
			}
		})
	}
}
