package build_dependence

import (
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"golang.org/x/tools/go/packages"
)

// The records' wide run's variables (#6a3kjvz): the tree it loads, and the builds it judges, by name (wideBuilds),
// every one when it names none.
const (
	recordsTreeVariable   = "COHERE_GO_BUILD_DEPENDENCE_RECORDS"
	recordsBuildsVariable = "COHERE_GO_BUILD_DEPENDENCE_RECORDS_BUILDS"
)

// recordedGraphOf is a graph of every package live holds, each added from its record's bytes (EncodedRecord,
// AddRecord), so it holds no syntax, no types and no live object.
func recordedGraphOf(test *testing.T, live *Graph) *Graph {
	test.Helper()
	recorded := NewGraph()
	for _, identifier := range slices.Sorted(maps.Keys(live.packages)) {
		encoded, isHeld := live.EncodedRecord(identifier)
		if !isHeld {
			test.Fatalf("the live graph holds no record of %s", identifier)
		}
		if addError := recorded.AddRecord(encoded); addError != nil {
			test.Fatalf("adding the record of %s: %v", identifier, addError)
		}
	}
	return recorded
}

// resolvedLines are graph's resolution as lines that name no index: each unit by its place, with its size, its
// declared objects, its receiver, its bits, its external reads and its calls, each by key; then every kind edge,
// bit to bit, by key. Two graphs resolve alike exactly when their lines are equal.
func resolvedLines(graph *Graph) []string {
	resolved := graph.resolve()
	keys := make([]nodeKey, len(resolved.objects))
	for key, node := range resolved.nodes {
		keys[node] = key
	}
	bitName := func(value int32) string {
		key := keys[value/2]
		return fmt.Sprintf("%s %s/%d", resolved.packages[key.packageIndex].identifier, key.local, value%2)
	}
	unitName := func(index int) string {
		return fmt.Sprintf("%s %s:%d", resolved.packages[resolved.unitPackage[index]].identifier,
			resolved.files.names[resolved.unitFile[index]], resolved.unitStart[index])
	}
	var lines []string
	for index := range resolved.unitCount() {
		parts := []string{"unit " + unitName(index), fmt.Sprintf("nodes %d end %d universe %x",
			resolved.unitNodes[index], resolved.unitEnd[index], resolved.unitUniverseNames[index])}
		for _, node := range resolved.declares.row(index) {
			parts = append(parts, "declares "+bitName(bit(node, false)))
		}
		if receiver := resolved.unitReceiver[index]; receiver >= 0 {
			parts = append(parts, "receiver "+bitName(bit(receiver, false)))
		}
		var reads []string
		for _, read := range resolved.bits.row(index) {
			reads = append(reads, "reads "+bitName(read))
		}
		for _, read := range resolved.external.row(index) {
			object := &resolved.externalObjects[read>>1]
			reads = append(reads, fmt.Sprintf("external %s %s/%t", object.path, object.local, read&1 != 0))
		}
		for _, called := range resolved.calls.row(index) {
			reads = append(reads, "calls "+unitName(int(called)))
		}
		slices.Sort(reads)
		lines = append(lines, strings.Join(append(parts, slices.Compact(reads)...), "; "))
	}
	for to, dependents := range resolved.dependents {
		for _, from := range dependents {
			lines = append(lines, "edge "+bitName(int32(to))+" <- "+bitName(from))
		}
	}
	for _, edge := range resolved.externalEdges {
		object := &resolved.externalObjects[edge.to>>1]
		lines = append(lines, fmt.Sprintf("external edge %s <- %s %s/%t", bitName(edge.fromBit), object.path,
			object.local, edge.to&1 != 0))
	}
	slices.Sort(lines)
	return slices.Compact(lines)
}

// unitVerdicts are dependence's verdicts, each unit by its place, and its counts.
func unitVerdicts(dependence *Dependence) []string {
	var lines []string
	for _, unit := range dependence.Units() {
		lines = append(lines, fmt.Sprintf("%s %s:%d %t", unit.PackageId, unit.File, unit.Start, unit.Dependent))
	}
	lines = append(lines, fmt.Sprintf("counts %+v", dependence.Counts()))
	return lines
}

// expectAlike fails, under label, for every line one of live and recorded has and the other does not, the first few
// of each listed, and reports how many lines both have.
func expectAlike(test *testing.T, label string, live []string, recorded []string) int {
	test.Helper()
	inLive, inRecorded := map[string]bool{}, map[string]bool{}
	for _, line := range live {
		inLive[line] = true
	}
	for _, line := range recorded {
		inRecorded[line] = true
	}
	var liveOnly []string
	var recordedOnly []string
	for _, line := range live {
		if !inRecorded[line] {
			liveOnly = append(liveOnly, line)
		}
	}
	for _, line := range recorded {
		if !inLive[line] {
			recordedOnly = append(recordedOnly, line)
		}
	}
	if len(liveOnly) > 0 || len(recordedOnly) > 0 {
		test.Errorf("%s: %d lines only the live graph has and %d only the recorded one has; the first:\n%s\n---\n%s",
			label, len(liveOnly), len(recordedOnly), strings.Join(liveOnly[:min(len(liveOnly), 8)], "\n"),
			strings.Join(recordedOnly[:min(len(recordedOnly), 8)], "\n"))
	}
	return len(live)
}

// expectNoUnnameable fails when a record of graph read an object of another package no name can carry, which a
// record read back could not find.
func expectNoUnnameable(test *testing.T, label string, graph *Graph) {
	test.Helper()
	if unnameable := graph.Unnameable(); unnameable > 0 {
		test.Errorf("%s: the records read %d objects of other packages that no name can carry", label, unnameable)
	}
}

// A graph of every package added from its record's bytes resolves as the graph of the live packages does, every
// unit, object, bit, edge and call by its key, and judges every build the same, unit for unit, over block 1's
// fixtures, the layout fixture of step 3 among them, under linux and under a tag. No record reads an object of
// another package it cannot name. Drop one kind of edge from the encoding, those to an underlying bit say, and the
// lines differ.
func TestARecordedGraphIsTheLiveGraph(test *testing.T) {
	test.Parallel()

	for _, fixture := range []string{"layout", "methods", "lowering"} {
		host := loadFixture(test, fixture, hostBuild)
		live := graphOf(host, nil)
		expectNoUnnameable(test, fixture, live)
		recorded := recordedGraphOf(test, live)
		expectAlike(test, fixture+" resolved", resolvedLines(live), resolvedLines(recorded))
		for _, build := range []fixtureBuild{linuxBuild, taggedBuild} {
			difference := differenceOf(host, loadFixture(test, fixture, build))
			expectAlike(test, fixture+" under "+build.name, unitVerdicts(live.Against(difference)),
				unitVerdicts(recorded.Against(difference)))
		}
	}
}

// A record read back is the record written: encoding a decoded record gives the same bytes, and bytes that end
// early, or are of another format, are refused rather than read wrong.
func TestARecordReadsBackWhole(test *testing.T) {
	test.Parallel()

	live := graphOf(loadFixture(test, "layout", hostBuild), nil)
	for _, identifier := range slices.Sorted(maps.Keys(live.packages)) {
		encoded, _ := live.EncodedRecord(identifier)
		decoded, decodeError := decodeRecord(encoded)
		if decodeError != nil {
			test.Fatalf("%s: %v", identifier, decodeError)
		}
		if again := encodeRecord(decoded); string(again) != string(encoded) {
			test.Errorf("%s: a decoded record encodes to %d bytes, not its own %d", identifier, len(again),
				len(encoded))
		}
		if _, shortError := decodeRecord(encoded[:len(encoded)-1]); shortError == nil {
			test.Errorf("%s: a record cut short by a byte decodes", identifier)
		}
	}
	if _, formatError := decodeRecord([]byte("another format")); formatError == nil {
		test.Error("bytes of another format decode as a record")
	}
}

// The wide run: the tree COHERE_GO_BUILD_DEPENDENCE_RECORDS names, loaded whole under the host, its graph built from
// the live packages and again from their records, the two resolved alike and judged alike under each build
// COHERE_GO_BUILD_DEPENDENCE_RECORDS_BUILDS names, every build when it names none, and no record reading an object of
// another package it cannot name.
func TestARecordedGraphIsTheLiveGraphInATree(test *testing.T) {
	test.Parallel()

	tree := os.Getenv(recordsTreeVariable)
	if tree == "" {
		test.Skipf("%s names no tree", recordsTreeVariable)
	}
	started := time.Now()
	host := loadTree(test, tree, wideEnvironment("darwin", ""))
	live := graphOf(host, nil)
	expectNoUnnameable(test, tree, live)
	encodedBytes := 0
	for identifier := range live.packages {
		encoded, _ := live.EncodedRecord(identifier)
		encodedBytes += len(encoded)
	}
	recorded := recordedGraphOf(test, live)
	lines := expectAlike(test, tree+" resolved", resolvedLines(live), resolvedLines(recorded))
	test.Logf("host darwin: %d packages, %d records in %d bytes, %d objects of other packages no name carries, %d "+
		"resolved lines alike, in %s", len(host), len(live.packages), encodedBytes, live.Unnameable(), lines,
		time.Since(started).Round(time.Second))
	names := slices.Sorted(maps.Keys(wideBuilds))
	if named := os.Getenv(recordsBuildsVariable); named != "" {
		names = strings.Split(named, ",")
	}
	for _, name := range names {
		wide, isKnown := wideBuilds[name]
		if !isKnown {
			test.Fatalf("no build named %q; the builds are %q", name, slices.Sorted(maps.Keys(wideBuilds)))
		}
		started := time.Now()
		difference := differenceOf(host, loadTree(test, tree, wideEnvironment(wide.goos, wide.tags)))
		liveVerdicts := unitVerdicts(live.Against(difference))
		units := expectAlike(test, tree+" under "+name, liveVerdicts, unitVerdicts(recorded.Against(difference)))
		test.Logf("build %s: %d unit verdicts and counts alike, %s", name, units, liveVerdicts[len(liveVerdicts)-1])
		test.Logf("build %s: in %s", name, time.Since(started).Round(time.Second))
	}
}

// A record read back names an object of a package its graph does not hold by its path and local name, where a live
// record holds the object, and the other graph's judgment answers the name as it answers the object (#6a3kjvz): the
// layout fixture's workspace, its graph built from records of packages restored from export data, judges every unit
// on linux as the live graph does with each object answered by its counterpart. Answered by name with no
// DependentName, every such read fails wide, and Labelled, independent in truth, is called dependent.
func TestARecordedGraphAnswersByNameAsTheLiveGraphByObject(test *testing.T) {
	test.Parallel()

	judged := judge(test, "layout", linuxBuild)
	restored := restoredFromExportData(test, judged.host)
	dependencyGraph := graphOf(judged.host, func(loaded *packages.Package) bool { return fixtureWorkspace(loaded) })
	dependencyJudged := dependencyGraph.Against(judged.difference)
	live := graphOf(restored, nil)
	recorded := recordedGraphOf(test, live)

	byObject := judged.difference
	byObject.DependentObject = NewCounterparts(dependencyGraph, dependencyJudged).DependentObject
	liveVerdicts := verdicts(test, restored, live.Against(byObject))
	byName := judged.difference
	named := NewCounterparts(dependencyGraph, dependencyJudged)
	byName.DependentObject, byName.DependentName = named.DependentObject, named.DependentName
	recordedVerdicts := verdicts(test, restored, recorded.Against(byName))
	for name, dependent := range liveVerdicts {
		if recordedVerdicts[name] != dependent {
			test.Errorf("%s: the recorded graph judges dependent %t by name, the live graph %t by object", name,
				recordedVerdicts[name], dependent)
		}
	}
	if counts := named.Counts(); counts.Answered == 0 || counts.WithoutPackage != 0 {
		test.Errorf("want every name answered by the dependency's graph, got %+v", counts)
	}

	failingWide := judged.difference
	failingWide.DependentObject = named.DependentObject
	if !verdicts(test, restored, recorded.Against(failingWide))["func Labelled"] {
		test.Error("func Labelled: want it dependent, every read failing wide with no DependentName, got independent")
	}
}

// A record read back keeps each static call into a package its graph does not hold, by the callee's path and local
// name, as a live record does (#6a3kjvz), so the facts census finds the same callers either way: the layout fixture's
// workspace, restored from export data, calls functions of its dependency, and every unit of its graph from records
// carries the calls the live graph's unit carries, each declared by a unit of the dependency's graph.
func TestARecordedGraphKeepsItsCallsIntoAnotherGraph(test *testing.T) {
	test.Parallel()

	judged := judge(test, "layout", linuxBuild)
	restored := restoredFromExportData(test, judged.host)
	dependencyGraph := graphOf(judged.host, func(loaded *packages.Package) bool { return fixtureWorkspace(loaded) })
	dependencyJudged := dependencyGraph.Against(judged.difference)
	live := graphOf(restored, nil)
	callsOf := func(graph *Graph) map[string][]ObjectName {
		calls := map[string][]ObjectName{}
		for _, unit := range graph.Against(judged.difference).Units() {
			if len(unit.ExternalCalls) > 0 {
				calls[fmt.Sprintf("%s %s:%d", unit.PackageId, unit.File, unit.Start)] = unit.ExternalCalls
			}
		}
		return calls
	}
	liveCalls, recordedCalls := callsOf(live), callsOf(recordedGraphOf(test, live))
	if len(liveCalls) == 0 {
		test.Fatal("no unit of the workspace calls its dependency, so the case proves nothing")
	}
	for unit, calls := range liveCalls {
		if !slices.Equal(recordedCalls[unit], calls) {
			test.Errorf("%s: the recorded graph's unit calls %v outside it, the live graph's %v", unit,
				recordedCalls[unit], calls)
		}
		for _, callee := range calls {
			if _, isDeclared := dependencyJudged.UnitDeclaringName(callee.Path, callee.Local); !isDeclared {
				test.Errorf("%s calls %v, which no unit of the dependency's graph declares", unit, callee)
			}
		}
	}
}
