package build_dependence

import (
	"fmt"
	"maps"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/needs"
)

// declarationsTreesVariable names the trees the declarations-only graph's wide run loads (#w72zxtq), comma-separated,
// `std` standing for the standard library's own source.
const declarationsTreesVariable = "COHERE_GO_BUILD_DEPENDENCE_DECLARATIONS"

// declarationsListed is how many differing lines or bits a comparison lists, beside their count.
const declarationsListed = 8

// declarationsTally is what holding a declarations-only graph to a whole one of the same packages found: the whole
// graph's declaration lines (declarationLines), those of them kind edges, and the lines one graph has and the other
// does not; then, under one build, the nodes compared, the bits the whole graph's judgment sets, and the bits the two
// judgments set differently.
type declarationsTally struct {
	lines       int
	edgeLines   int
	linesDiffer int
	nodes       int
	bitsSet     int
	bitsMoved   int
}

// String is the tally in one line.
func (tally declarationsTally) String() string {
	return fmt.Sprintf("%d nodes compared, %d bits set, %d bits moved; %d declaration lines, %d of them kind edges, "+
		"%d differ", tally.nodes, tally.bitsSet, tally.bitsMoved, tally.lines, tally.edgeLines, tally.linesDiffer)
}

// declarationsGraphOf is graphOf's graph of host's packages, collecting declarations only (NewDeclarationsGraph).
func declarationsGraphOf(host loadedBuild, excluded func(*packages.Package) bool) *Graph {
	graph := NewDeclarationsGraph()
	var group sync.WaitGroup
	for _, loaded := range host {
		if excluded != nil && excluded(loaded) {
			continue
		}
		group.Go(func() { graph.AddPackage(loaded, fixtureWorkspace(loaded), nil) })
	}
	group.Wait()
	return graph
}

// declarationLines are graph's resolution as resolvedLines writes it, less what only a unit's body gives, its reads,
// its external reads and its calls: every unit by its place, with its size, its universe names, its declared objects
// and its receiver, and every kind edge.
func declarationLines(graph *Graph) []string {
	lines := resolvedLines(graph)
	for index, line := range lines {
		if !strings.HasPrefix(line, "unit ") {
			continue
		}
		parts := slices.DeleteFunc(strings.Split(line, "; "), func(part string) bool {
			return strings.HasPrefix(part, "reads ") || strings.HasPrefix(part, "external ") ||
				strings.HasPrefix(part, "calls ")
		})
		lines[index] = strings.Join(parts, "; ")
	}
	return lines
}

// linesOnly are the lines of lines that other does not hold.
func linesOnly(lines []string, other []string) []string {
	held := make(map[string]bool, len(other))
	for _, line := range other {
		held[line] = true
	}
	var only []string
	for _, line := range lines {
		if !held[line] {
			only = append(only, line)
		}
	}
	return only
}

// expectDeclarationsAlike fails, under label, for every declaration line (declarationLines) one of whole and
// declarations has and the other does not, the first few of each listed, and tallies the whole graph's lines.
func expectDeclarationsAlike(test *testing.T, label string, whole *Graph, declarations *Graph) declarationsTally {
	test.Helper()
	wholeLines, declarationsLines := declarationLines(whole), declarationLines(declarations)
	tally := declarationsTally{lines: len(wholeLines)}
	for _, line := range wholeLines {
		if strings.HasPrefix(line, "edge ") || strings.HasPrefix(line, "external edge ") {
			tally.edgeLines++
		}
	}
	wholeOnly, declarationsOnly := linesOnly(wholeLines, declarationsLines), linesOnly(declarationsLines, wholeLines)
	tally.linesDiffer = len(wholeOnly) + len(declarationsOnly)
	if tally.linesDiffer > 0 {
		test.Errorf("%s: %d declaration lines only the whole graph has and %d only the declarations-only one has; "+
			"the first:\n%s\n---\n%s", label, len(wholeOnly), len(declarationsOnly),
			strings.Join(wholeOnly[:min(len(wholeOnly), declarationsListed)], "\n"),
			strings.Join(declarationsOnly[:min(len(declarationsOnly), declarationsListed)], "\n"))
	}
	return tally
}

// expectBitsAlike judges difference on whole and on declarations, two graphs of the same packages, and fails, under
// label, for every node one has and the other does not and for every bit the two judgments set differently, the first
// few listed. It tallies the nodes compared, the bits the whole graph's judgment sets, and those that differ.
func expectBitsAlike(
	test *testing.T,
	label string,
	whole *Graph,
	declarations *Graph,
	difference Difference,
) declarationsTally {
	test.Helper()
	var tally declarationsTally
	wholeJudged, declarationsJudged := whole.Against(difference), declarations.Against(difference)
	if len(wholeJudged.resolved.nodes) != len(declarationsJudged.resolved.nodes) {
		test.Errorf("%s: the whole graph has %d nodes and the declarations-only one %d", label,
			len(wholeJudged.resolved.nodes), len(declarationsJudged.resolved.nodes))
	}
	var moved []string
	for key, node := range wholeJudged.resolved.nodes {
		identifier := wholeJudged.resolved.packages[key.packageIndex].identifier
		other, isNode := declarationsJudged.resolved.nodes[nodeKey{
			packageIndex: int32(declarationsJudged.resolved.packageIndex[identifier]), local: key.local}]
		if !isNode {
			test.Errorf("%s: %s %s has no node in the declarations-only graph", label, identifier, key.local)
			continue
		}
		tally.nodes++
		for _, underlying := range []bool{false, true} {
			set := wholeJudged.bits[bit(node, underlying)]
			if set {
				tally.bitsSet++
			}
			if declarationsJudged.bits[bit(other, underlying)] != set {
				tally.bitsMoved++
				moved = append(moved, fmt.Sprintf("%s %s, underlying %t: set %t in the whole graph", identifier,
					key.local, underlying, set))
			}
		}
	}
	if tally.bitsMoved > 0 {
		slices.Sort(moved)
		test.Errorf("%s: %d bits differ in the declarations-only graph, the first:\n%s", label, tally.bitsMoved,
			strings.Join(moved[:min(len(moved), declarationsListed)], "\n"))
	}
	return tally
}

// A declarations-only graph gives every declared object the two bits a whole graph gives it, and resolves to the same
// units' places, sizes, universe names and declared objects and the same kind edges, over block 1's fixtures under the
// builds its tests judge each by, and the shadowed fixture, whose seed comes from a body's name. ObjectBits of every
// object a fixture defines or uses and DependentObjects answer alike too. Every fixture sets a bit under some build,
// so none is compared with nothing. Walk only a function's header for its unit's size and names in the mode and the
// lines of every fixture that declares a function differ, the shadowed fixture's bits with them; leave out the walk
// of a var's initializers for the types they pass through and the inferred fixture's edges and bits differ.
func TestADeclarationsGraphGivesEveryObjectAWholeGraphsBits(test *testing.T) {
	test.Parallel()

	windowsBuild := fixtureBuild{name: "windows", environment: buildEnvironment("windows", "")}
	for _, walked := range []struct {
		fixture string
		builds  []fixtureBuild
	}{
		{"layout", []fixtureBuild{linuxBuild, taggedBuild}},
		{"methods", []fixtureBuild{linuxBuild, taggedBuild}},
		{"lowering", []fixtureBuild{linuxBuild, taggedBuild}},
		{"lengths", []fixtureBuild{windowsBuild}},
		{"inferred", []fixtureBuild{windowsBuild}},
		{"aliases", []fixtureBuild{linuxBuild}},
		{"sharedlocalaliases", []fixtureBuild{linuxBuild}},
		{"localaliases", []fixtureBuild{linuxBuild}},
		{"closures", []fixtureBuild{linuxBuild}},
		{"shadowed", []fixtureBuild{linuxBuild}},
	} {
		test.Run(walked.fixture, func(test *testing.T) {
			test.Parallel()

			host := loadFixture(test, walked.fixture, hostBuild)
			whole, declarations := graphOf(host, nil), declarationsGraphOf(host, nil)
			lines := expectDeclarationsAlike(test, walked.fixture, whole, declarations)
			objects := definedAndUsed(host)
			bitsSet := 0
			for _, build := range walked.builds {
				label := walked.fixture + " under " + build.name
				difference := differenceOf(host, loadFixture(test, walked.fixture, build))
				tally := expectBitsAlike(test, label, whole, declarations, difference)
				tally.lines, tally.edgeLines, tally.linesDiffer = lines.lines, lines.edgeLines, lines.linesDiffer
				test.Logf("%s: %s", label, tally)
				bitsSet += tally.bitsSet
				// The same through the answers a caller reads, an object no unit declares, read through its type, among
				// them.
				wholeJudged, declarationsJudged := whole.Against(difference), declarations.Against(difference)
				for _, object := range objects {
					if want, got := wholeJudged.ObjectBits(object), declarationsJudged.ObjectBits(object); got != want {
						test.Errorf("%s: want ObjectBits of %s %+v as the whole graph answers, got %+v", label,
							objectLabel(object), want, got)
					}
				}
				if !maps.Equal(wholeJudged.DependentObjects(), declarationsJudged.DependentObjects()) {
					test.Errorf("%s: want the whole graph's %d dependent objects, got %d or others", label,
						len(wholeJudged.DependentObjects()), len(declarationsJudged.DependentObjects()))
				}
			}
			if lines.edgeLines == 0 || bitsSet == 0 {
				test.Errorf("%s: %d kind edges and %d bits set under its builds, so the comparison compared nothing",
					walked.fixture, lines.edgeLines, bitsSet)
			}
		})
	}
}

// On linux a file only the build compiles declares min at package level, so the name means another object in every
// file of the shadowed fixture's package. Smaller uses min in its body alone, and is seeded, both its bits set; Least,
// a var that takes its value from Smaller, follows it by its kind edge; Untouched uses no name the build declares. A
// declarations-only graph sets the same bits, since a unit's universe names are read from its whole declaration,
// body included, in either mode (newUnit).
func TestANameOfTheUniverseABodyUsesSeedsItsUnitInADeclarationsGraph(test *testing.T) {
	test.Parallel()

	judged := judge(test, "shadowed", linuxBuild)
	expectVerdicts(test, verdicts(test, judged.host, judged.dependence), map[string]bool{
		"func Smaller":   true,
		"value Least":    true,
		"func Untouched": false,
	})
	declarations := declarationsGraphOf(judged.host, nil).Against(judged.difference)
	for _, name := range []struct {
		local string
		want  Bits
	}{
		{"Smaller", Bits{Name: true, Underlying: true}},
		{"Least", Bits{Name: true}},
		{"Untouched", Bits{}},
	} {
		for _, judgment := range []struct {
			graph      string
			dependence *Dependence
		}{{"whole", judged.dependence}, {"declarations-only", declarations}} {
			bits, isDeclared := judgment.dependence.NameBits("example.com/shadowed", name.local)
			if !isDeclared || bits != name.want {
				test.Errorf("%s: want its bits %+v on linux in the %s graph, got %+v, declared %t", name.local,
					name.want, judgment.graph, bits, isDeclared)
			}
		}
	}
}

// A declarations-only graph collects no unit's body: where the whole graph's resolution holds units' reads, their
// reads of packages it does not hold, their calls and their calls into such packages, the declarations-only graph's
// holds none of any. The layout fixture's workspace, without its dependency, gives all four from function bodies, and
// the inferred fixture's vars give reads and calls from initializers alone, its one function's body reading nothing.
// Without this a mode that changed nothing would pass the identity above.
func TestADeclarationsGraphCollectsNoBody(test *testing.T) {
	test.Parallel()

	for _, collected := range []struct {
		fixture  string
		excluded func(*packages.Package) bool
		holds    []string
	}{
		{"layout", func(loaded *packages.Package) bool { return !fixtureWorkspace(loaded) },
			[]string{"reads", "external reads", "calls", "external calls"}},
		{"inferred", nil, []string{"reads", "calls"}},
	} {
		host := loadFixture(test, collected.fixture, hostBuild)
		whole := graphOf(host, collected.excluded).resolve()
		declarations := declarationsGraphOf(host, collected.excluded).resolve()
		entries := func(resolved *resolvedGraph) map[string]int {
			return map[string]int{
				"reads":                     len(resolved.bits.entries),
				"external reads":            len(resolved.external.entries),
				"what external reads reach": len(resolved.externalThrough.entries),
				"calls":                     len(resolved.calls.entries),
				"external calls":            len(resolved.externalCalls.entries),
			}
		}
		wholeEntries, declarationsEntries := entries(whole), entries(declarations)
		for _, rows := range collected.holds {
			if wholeEntries[rows] == 0 {
				test.Errorf("%s: the whole graph holds no %s, so the case proves nothing of them", collected.fixture,
					rows)
			}
		}
		for _, rows := range slices.Sorted(maps.Keys(declarationsEntries)) {
			if declarationsEntries[rows] != 0 {
				test.Errorf("%s: want no %s in the declarations-only graph, got %d, the whole graph's %d",
					collected.fixture, rows, declarationsEntries[rows], wholeEntries[rows])
			}
		}
		if declarations.unitCount() != whole.unitCount() || declarations.unitCount() == 0 {
			test.Errorf("%s: want the whole graph's %d units in the declarations-only graph, got %d",
				collected.fixture, whole.unitCount(), declarations.unitCount())
		}
	}
}

// What only a unit's body gives, a declarations-only graph refuses with a panic that says what the caller tried:
// encoding a record, a unit's verdict, every unit, the counts, a call index over it, as the host's graph or as the
// standard library's, and a unit's verdict through a call index of whole graphs handed its judgment, as the host's or
// as the standard library's. A whole graph answers each. A refusal is told from any other panic by its opening words,
// since without one Units and UnitDependent would panic all the same, on a verdict that was never made. The refusals
// leave the graph's lock free, and what a declaration gives is still answered.
func TestADeclarationsGraphRefusesWhatOnlyABodyGives(test *testing.T) {
	test.Parallel()

	host := loadFixture(test, "layout", hostBuild)
	difference := differenceOf(host, loadFixture(test, "layout", linuxBuild))
	other := graphOf(host, nil)
	otherJudged := other.Against(difference)
	wholeIndex := NewCallIndex(other, graphOf(host, nil))
	for _, asked := range []struct {
		name    string
		graph   *Graph
		refuses bool
	}{
		{"a whole graph", graphOf(host, nil), false},
		{"a declarations-only graph", declarationsGraphOf(host, nil), true},
	} {
		dependence := asked.graph.Against(difference)
		resolved := dependence.resolved
		if resolved.unitCount() == 0 {
			test.Fatalf("%s: the layout fixture's graph holds no unit", asked.name)
		}
		packageId := resolved.packages[resolved.unitPackage[0]].identifier
		file, start := resolved.files.names[resolved.unitFile[0]], int(resolved.unitStart[0])
		for _, refused := range []struct {
			name  string
			tried string
			call  func()
		}{
			{"EncodedRecord", "encode a record of", func() { _, _ = asked.graph.EncodedRecord(packageId) }},
			{"UnitDependent", "read a unit's verdict from",
				func() { dependence.UnitDependent(packageId, file, start) }},
			{"Units", "read every unit's verdict and calls from", func() { dependence.Units() }},
			{"Counts", "read the counts of", func() { dependence.Counts() }},
			{"NewCallIndex with it as the host's", "build a call index over",
				func() { NewCallIndex(asked.graph, nil) }},
			{"NewCallIndex with it as the standard library's", "build a call index over",
				func() { NewCallIndex(other, asked.graph) }},
			{"CallIndex.Dependent with its judgment as the host's", "read a unit's verdict through a call index from",
				func() { wholeIndex.Dependent(dependence, otherJudged, 0) }},
			{"CallIndex.Dependent with its judgment as the standard library's",
				"read a unit's verdict through a call index from",
				func() { wholeIndex.Dependent(otherJudged, dependence, 0) }},
		} {
			message := panicOf(refused.call)
			want := "build_dependence: a caller tried to " + refused.tried + " a declarations-only graph"
			switch {
			case asked.refuses && !strings.HasPrefix(message, want):
				test.Errorf("%s, %s: want a panic opening %q, got %q", asked.name, refused.name, want, message)
			case !asked.refuses && message != "":
				test.Errorf("%s, %s: want it answered, got the panic %q", asked.name, refused.name, message)
			}
		}
		if unnameable := asked.graph.Unnameable(); unnameable != 0 {
			test.Errorf("%s: want the lock free and no unnameable object after the refusals, got %d", asked.name,
				unnameable)
		}
		if _, isUnit := dependence.UnitContaining(packageId, file, start); !isUnit {
			test.Errorf("%s: want the unit at %s:%d still found by its place", asked.name, file, start)
		}
	}
}

// A whole graph's records added to a declarations-only graph, which holds more than it reads of each, resolve to the
// whole graph's declarations and give every object its bits, under linux and under a tag.
func TestADeclarationsGraphTakesAWholeGraphsRecords(test *testing.T) {
	test.Parallel()

	host := loadFixture(test, "layout", hostBuild)
	whole := graphOf(host, nil)
	recorded := NewDeclarationsGraph()
	for _, identifier := range slices.Sorted(maps.Keys(whole.packages)) {
		encoded, _ := whole.EncodedRecord(identifier)
		if addError := recorded.AddRecord(encoded); addError != nil {
			test.Fatalf("adding the record of %s: %v", identifier, addError)
		}
	}
	expectDeclarationsAlike(test, "layout from records", whole, recorded)
	for _, build := range []fixtureBuild{linuxBuild, taggedBuild} {
		tally := expectBitsAlike(test, "layout from records under "+build.name, whole, recorded,
			differenceOf(host, loadFixture(test, "layout", build)))
		if tally.bitsSet == 0 {
			test.Errorf("layout from records under %s: no bit is set, so the comparison compared nothing", build.name)
		}
	}
}

// The wide run: each tree COHERE_GO_BUILD_DEPENDENCE_DECLARATIONS names, `std` for the standard library's own source,
// loaded whole under the host, its whole graph and its declarations-only graph held to each other as the fixtures'
// are, their declaration lines and then every object's bits under linux and under windows, one build's load at a time
// beside the host's. It logs each graph's add and each comparison's tally.
func TestADeclarationsGraphGivesEveryObjectAWholeGraphsBitsInATree(test *testing.T) {
	test.Parallel()

	trees := needs.OptIn(test, declarationsTreesVariable, "it names the trees, comma-separated, `std` for the "+
		"standard library's own source, whose declarations-only graph is held to its whole graph under linux and "+
		"windows")
	for tree := range strings.SplitSeq(trees, ",") {
		directory := tree
		if tree == "std" {
			root, rootError := exec.Command("go", "env", "GOROOT").Output()
			if rootError != nil {
				test.Fatal(rootError)
			}
			directory = filepath.Join(strings.TrimSpace(string(root)), "src")
		}
		started := time.Now()
		host := loadTree(test, directory, wideEnvironment("darwin", ""))
		loaded := time.Now()
		whole := graphOf(host, nil)
		wholeAdded := time.Now()
		declarations := declarationsGraphOf(host, nil)
		declarationsAdded := time.Now()
		lines := expectDeclarationsAlike(test, tree, whole, declarations)
		test.Logf("%s on the host: %d packages loaded in %s; the whole graph added in %s, the declarations-only one "+
			"in %s", tree, len(host), loaded.Sub(started).Round(time.Second),
			wholeAdded.Sub(loaded).Round(time.Millisecond), declarationsAdded.Sub(wholeAdded).Round(time.Millisecond))
		for _, name := range []string{"linux", "windows"} {
			wide := wideBuilds[name]
			tally := expectBitsAlike(test, tree+" under "+name, whole, declarations,
				differenceOf(host, loadTree(test, directory, wideEnvironment(wide.goos, wide.tags))))
			tally.lines, tally.edgeLines, tally.linesDiffer = lines.lines, lines.edgeLines, lines.linesDiffer
			test.Logf("%s under %s: %s", tree, name, tally)
			if tally.bitsSet == 0 {
				test.Errorf("%s under %s: no bit is set, so the comparison compared nothing", tree, name)
			}
		}
	}
}
