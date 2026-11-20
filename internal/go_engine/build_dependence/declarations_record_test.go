package build_dependence

import (
	"maps"
	"slices"
	"strings"
	"testing"
)

// declarationsRecordedGraphOf is a declarations-only graph of every package live holds, each added from its record's
// bytes (EncodedRecord, AddRecord), so it holds no syntax, no types and no live object.
func declarationsRecordedGraphOf(test *testing.T, live *Graph) *Graph {
	test.Helper()
	recorded := NewDeclarationsGraph()
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

// A declarations-only graph's record is a declarations-only record (#anfjtr3): its bytes open with that format's words,
// it decodes saying so, and encoding the decoded record gives the same bytes, while bytes cut short are refused. A
// whole graph's record of the same package decodes as a whole one, and is longer, holding its units' reads and calls.
func TestADeclarationsRecordReadsBackWhole(test *testing.T) {
	test.Parallel()

	host := loadFixture(test, "layout", hostBuild)
	declarations, whole := declarationsGraphOf(host, nil), graphOf(host, nil)
	identifiers := slices.Sorted(maps.Keys(declarations.packages))
	if len(identifiers) == 0 {
		test.Fatal("the layout fixture's graph holds no package")
	}
	for _, identifier := range identifiers {
		encoded, isHeld := declarations.EncodedRecord(identifier)
		if !isHeld {
			test.Fatalf("%s: the declarations-only graph holds no record of it", identifier)
		}
		if !strings.HasPrefix(string(encoded), declarationsRecordFormat+"\x00") {
			test.Errorf("%s: want the record to open with %q, got %q", identifier, declarationsRecordFormat,
				string(encoded[:min(len(encoded), len(declarationsRecordFormat))]))
		}
		decoded, decodeError := DecodeRecord(encoded)
		if decodeError != nil {
			test.Fatalf("%s: %v", identifier, decodeError)
		}
		if !decoded.DeclarationsOnly() {
			test.Errorf("%s: want the record decoded as declarations-only, got a whole one", identifier)
		}
		if again := encodeRecord(decoded.record); string(again) != string(encoded) {
			test.Errorf("%s: a decoded declarations-only record encodes to %d bytes, not its own %d", identifier,
				len(again), len(encoded))
		}
		if _, shortError := DecodeRecord(encoded[:len(encoded)-1]); shortError == nil {
			test.Errorf("%s: a declarations-only record cut short by a byte decodes", identifier)
		}
		wholeEncoded, _ := whole.EncodedRecord(identifier)
		wholeDecoded, wholeError := DecodeRecord(wholeEncoded)
		if wholeError != nil {
			test.Fatalf("%s: %v", identifier, wholeError)
		}
		if wholeDecoded.DeclarationsOnly() {
			test.Errorf("%s: want the whole graph's record decoded as whole, got a declarations-only one", identifier)
		}
		if len(wholeEncoded) <= len(encoded) {
			test.Errorf("%s: want the whole record, holding its units' reads and calls, longer than the "+
				"declarations-only one's %d bytes, got %d", identifier, len(encoded), len(wholeEncoded))
		}
	}
}

// A whole graph refuses a declarations-only record with a panic that says what the caller tried, so no unit dependent
// by its body alone is called independent, and the refusal leaves its lock free and the package out. A
// declarations-only graph takes the same bytes.
func TestAWholeGraphRefusesADeclarationsRecord(test *testing.T) {
	test.Parallel()

	declarations := declarationsGraphOf(loadFixture(test, "layout", hostBuild), nil)
	identifier := slices.Sorted(maps.Keys(declarations.packages))[0]
	encoded, _ := declarations.EncodedRecord(identifier)
	whole := NewGraph()
	message := panicOf(func() {
		if addError := whole.AddRecord(encoded); addError != nil {
			test.Errorf("a whole graph adding the record: want a panic, got the error %v", addError)
		}
	})
	want := "build_dependence: a caller tried to add the declarations-only record of " + identifier +
		" to a whole graph"
	if !strings.HasPrefix(message, want) {
		test.Errorf("want a panic opening %q, got %q", want, message)
	}
	if unnameable := whole.Unnameable(); unnameable != 0 || len(whole.packages) != 0 {
		test.Errorf("want the whole graph's lock free and no package held after the refusal, got %d packages",
			len(whole.packages))
	}
	taking := NewDeclarationsGraph()
	if message := panicOf(func() {
		if addError := taking.AddRecord(encoded); addError != nil {
			test.Errorf("a declarations-only graph adding the record: %v", addError)
		}
	}); message != "" {
		test.Errorf("want a declarations-only graph to take the record, got the panic %q", message)
	}
	if _, isHeld := taking.packages[identifier]; !isHeld {
		test.Errorf("want the declarations-only graph to hold %s from its record", identifier)
	}
}

// A declarations-only graph of every package added from its declarations-only record resolves as the live
// declarations-only graph does, every unit, object and kind edge by its key, and judges every build alike, bit for bit
// and package for package (DependentPackages), over block 1's fixtures under the builds they are judged by, and its
// dependent packages are a whole graph's. Drop the kind edges from the declarations-only format and the lines and the
// bits differ.
func TestADeclarationsRecordedGraphIsTheLiveDeclarationsGraph(test *testing.T) {
	test.Parallel()

	windowsBuild := fixtureBuild{name: "windows", environment: buildEnvironment("windows", "")}
	for _, recorded := range []struct {
		fixture string
		builds  []fixtureBuild
	}{
		{"layout", []fixtureBuild{linuxBuild, taggedBuild}},
		{"methods", []fixtureBuild{linuxBuild, taggedBuild}},
		{"lowering", []fixtureBuild{linuxBuild, taggedBuild}},
		{"inferred", []fixtureBuild{windowsBuild}},
	} {
		test.Run(recorded.fixture, func(test *testing.T) {
			test.Parallel()

			host := loadFixture(test, recorded.fixture, hostBuild)
			live := declarationsGraphOf(host, nil)
			fromRecords := declarationsRecordedGraphOf(test, live)
			whole := graphOf(host, nil)
			expectAlike(test, recorded.fixture+" resolved", resolvedLines(live), resolvedLines(fromRecords))
			bitsSet := 0
			for _, build := range recorded.builds {
				label := recorded.fixture + " under " + build.name
				difference := differenceOf(host, loadFixture(test, recorded.fixture, build))
				bitsSet += expectBitsAlike(test, label, live, fromRecords, difference).bitsSet
				livePackages := live.Against(difference).DependentPackages()
				if recordedPackages := fromRecords.Against(difference).DependentPackages(); !maps.Equal(
					recordedPackages, livePackages) {
					test.Errorf("%s: want the live graph's dependent packages %v from records, got %v", label,
						slices.Sorted(maps.Keys(livePackages)), slices.Sorted(maps.Keys(recordedPackages)))
				}
				if wholePackages := whole.Against(difference).DependentPackages(); !maps.Equal(wholePackages,
					livePackages) {
					test.Errorf("%s: want the whole graph's dependent packages %v, got %v", label,
						slices.Sorted(maps.Keys(wholePackages)), slices.Sorted(maps.Keys(livePackages)))
				}
			}
			if bitsSet == 0 {
				test.Errorf("%s: no bit is set under its builds, so the comparison compared nothing", recorded.fixture)
			}
		})
	}
}

// DependentPackages names a package the graph holds by a record read back, whose objects are names with no live
// object: the layout fixture's two packages from records, whole or declarations-only, are both dependent on linux,
// where Size and Width differ, and only the workspace's under the tag, where Mode alone does, as the live graph judges
// them. DependentObjects names no object of either, so it could not have. Skip the nodes with no live object and the
// recorded graphs name nothing.
func TestDependentPackagesNameADecodedPackage(test *testing.T) {
	test.Parallel()

	host := loadFixture(test, "layout", hostBuild)
	live := graphOf(host, nil)
	for _, graph := range []struct {
		name  string
		graph *Graph
	}{
		{"the live graph", live},
		{"a whole graph from records", recordedGraphOf(test, live)},
		{"a declarations-only graph from records", declarationsRecordedGraphOf(test, declarationsGraphOf(host, nil))},
	} {
		for _, judged := range []struct {
			build fixtureBuild
			want  []string
		}{
			{linuxBuild, []string{"example.com/layout", "example.com/layout/dependency"}},
			{taggedBuild, []string{"example.com/layout"}},
		} {
			dependence := graph.graph.Against(differenceOf(host, loadFixture(test, "layout", judged.build)))
			if got := slices.Sorted(maps.Keys(dependence.DependentPackages())); !slices.Equal(got, judged.want) {
				test.Errorf("%s under %s: want the dependent packages %q, got %q", graph.name, judged.build.name,
					judged.want, got)
			}
			if graph.graph != live && len(dependence.DependentObjects()) != 0 {
				test.Errorf("%s under %s: want no live dependent object, got %d", graph.name, judged.build.name,
					len(dependence.DependentObjects()))
			}
		}
	}
}
