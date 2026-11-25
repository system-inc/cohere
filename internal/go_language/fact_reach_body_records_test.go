package go_language

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"testing"

	"github.com/system-inc/cohere/internal/go_language/check"
	"github.com/system-inc/cohere/internal/go_language/load"
	"github.com/system-inc/cohere/internal/go_language/rule"
	"github.com/system-inc/cohere/internal/go_language/rules"
	"github.com/system-inc/cohere/internal/go_language/rules/unused_code"
)

// bodyRecordsFiles are a project of one package of three files: one a `//line` directive renames at its one function,
// so that body is keyed by the directive's name and parsed from the file's own, and one that holds no body.
var bodyRecordsFiles = map[string]string{
	"project/go.mod": "module example.com/records\n\ngo 1.24\n",
	"project/plain.go": "// Package records holds a body keyed by its own file and one keyed by a directive's.\n" +
		"package records\n\nfunc Plain() int {\n\tvalue := 1\n\treturn value\n}\n",
	"project/lined.go":    "package records\n\n//line other.go:40\nfunc Lined() int {\n\treturn Plain()\n}\n",
	"project/declared.go": "package records\n\n// Declared is the file's one declaration, no body.\nvar Declared = 1\n",
}

// capturedBodies runs U1000 and SA4017, which runs ctrlflow, so the capture holds a side of the package, over the
// body records project with the facts' reach captured, its bodies recorded as recordBodies says, and returns the
// package's ID, what the capture holds of the package, and the project's root.
func capturedBodies(test *testing.T, recordBodies bool) (string, *check.FactReachPackage, string) {
	test.Helper()
	root := writtenFixture(test, bodyRecordsFiles)
	loaded, loadError := load.Packages(root, []string{"./..."}, []string{"GOWORK=off"}, nil)
	if loadError != nil || len(loaded.Diagnostics) > 0 || len(loaded.Packages) != 1 {
		test.Fatalf("loading the body records project: %v %+v", loadError, loaded)
	}
	var running []rule.Rule
	for _, registered := range rules.All() {
		if registered.Name == rule.Namespace+"U1000" || registered.Name == rule.Namespace+"SA4017" {
			running = append(running, registered)
		}
	}
	if len(running) != 2 {
		test.Fatalf("the registry holds %d of U1000 and SA4017, want both", len(running))
	}
	result, runError := check.Run(loaded.Packages, running, check.Options{FactReach: true, RecordBodies: recordBodies,
		BodyRecorder: unusedBodyRecorder})
	if runError != nil {
		test.Fatalf("running the rules: %v", runError)
	}
	identifier := loaded.Packages[0].ID
	captured := result.FactReach.Packages[identifier]
	if captured == nil {
		test.Fatal("the capture holds no side of the package")
	}
	return identifier, captured, root
}

// A package's side answers the host's record of a body by the package's ID and the body's key, the `//line`
// directive's name for Lined's, never the name its file was parsed under, with the sha256 of the file it was parsed
// from (#xeafx79): lined.go's for Lined, plain.go's for Plain. It answers nothing for another package's ID, a test
// variant's among them, nor for a body whose file it holds no digest of, nor for any body of a package whose host was
// not asked to record. It holds the records of its own package's ID only where the host recorded them, and answers
// the digest of each of its files, declared.go's though it holds no body, but none for another package's ID or a file
// it holds no digest of.
func TestTheHostsSideAnswersABodysRecordByItsKey(test *testing.T) {
	test.Parallel()
	identifier, captured, root := capturedBodies(test, true)
	hosted := &factReachPackage{identifier: identifier}
	hosted.holdBodies(capturedBodyRecords(captured))
	var side unused_code.BodyRecordHost = factReachHost{factReachPackage: hosted}
	if !side.RecordsHeld(identifier) || side.RecordsHeld(identifier+" [example.com/records.test]") {
		test.Errorf("the side holds its package's records %t and the test variant's %t, want only its own",
			side.RecordsHeld(identifier), side.RecordsHeld(identifier+" [example.com/records.test]"))
	}
	// The method rules' U1000 asks through unused_code.FileDigestHost.
	digests, isDigests := side.(unused_code.FileDigestHost)
	if !isDigests {
		test.Fatal("the side answers no file's digest")
	}
	declared := filepath.Join(root, "declared.go")
	if digest, isDigested := digests.FileDigest(identifier, declared); !isDigested ||
		digest != sha256.Sum256([]byte(bodyRecordsFiles["project/declared.go"])) {
		test.Errorf("the side answers declared.go's digest %t %x, want its sha256", isDigested, digest)
	}
	if _, isDigested := digests.FileDigest(identifier+" [example.com/records.test]", declared); isDigested {
		test.Error("the side answers declared.go's digest for the test variant's ID")
	}
	if _, isDigested := digests.FileDigest(identifier, filepath.Join(root, "absent.go")); isDigested {
		test.Error("the side answers a digest of a file the package does not hold")
	}
	for _, each := range []struct {
		name       string
		file       string
		offset     int
		parsedFile string
	}{
		{name: "Lined", file: filepath.Join(root, "other.go"), offset: 36, parsedFile: "lined.go"},
		{name: "Plain", file: filepath.Join(root, "plain.go"), offset: 103, parsedFile: "plain.go"},
	} {
		body, digest, isRecorded := side.BodyRecord(identifier, each.file, each.offset)
		if !isRecorded {
			test.Errorf("the side answers no record of %s at %s offset %d", each.name, each.file, each.offset)
			continue
		}
		if file, offset := body.Key(); file != each.file || offset != each.offset {
			test.Errorf("the side answers %s with the record keyed at %s offset %d, want %s offset %d", each.name,
				file, offset, each.file, each.offset)
		}
		parsedFile := filepath.Join(root, each.parsedFile)
		if digest != sha256.Sum256([]byte(bodyRecordsFiles["project/"+each.parsedFile])) ||
			body.ParsedFile != parsedFile {
			test.Errorf("the side answers %s parsed from %s with the digest %x, want %s's sha256", each.name,
				body.ParsedFile, digest, parsedFile)
		}
		if _, _, isOther := side.BodyRecord(identifier+" [example.com/records.test]", each.file,
			each.offset); isOther {
			test.Errorf("the side answers %s's record for the test variant's ID", each.name)
		}
	}
	if _, _, isParsed := side.BodyRecord(identifier, filepath.Join(root, "lined.go"), 36); isParsed {
		test.Error("the side answers Lined's record by the name its file was parsed under")
	}

	undigested := &factReachPackage{identifier: identifier}
	recorded, _ := capturedBodyRecords(captured)
	undigested.holdBodies(unused_code.BodyRecords{Bodies: recorded.Bodies}, true)
	if _, _, isRecorded := undigested.BodyRecord(identifier, filepath.Join(root, "plain.go"), 103); isRecorded {
		test.Error("a side holding no digest of plain.go answers Plain's record")
	}

	unaskedIdentifier, unasked, unaskedRoot := capturedBodies(test, false)
	unaskedSide := &factReachPackage{identifier: unaskedIdentifier}
	unaskedSide.holdBodies(capturedBodyRecords(unasked))
	if _, _, isRecorded := unaskedSide.BodyRecord(unaskedIdentifier, filepath.Join(unaskedRoot, "plain.go"),
		103); isRecorded {
		test.Error("the side of a host not asked to record answers Plain's record")
	}
	if unaskedSide.RecordsHeld(unaskedIdentifier) {
		test.Error("the side of a host not asked to record holds its records")
	}
}

// On the census path with the standing bodies' switch on, the host's U1000 records its bodies and the side windows's
// build hands its passes answers each, as the standing bodies fixture's PlantedKept, which stands there, with the
// sha256 of standing.go (#xeafx79), and holds the package's records. With the switch off no build leaves a body
// standing, so the host records nothing, and the side answers no body and holds no records.
func TestTheSideABuildIsHandedAnswersAStandingBodysRecord(test *testing.T) {
	test.Parallel()
	for _, switched := range []bool{true, false} {
		engine, _, _, root := runStanding(test, switched, &heldRepresentations{})
		if len(engine.buildRuns) != 2 || engine.buildRuns[1].bodyReach == nil {
			test.Fatalf("switched %t, want the host's build and windows's census, got %d runs", switched,
				len(engine.buildRuns))
		}
		handed, isHanded := engine.buildRuns[1].factHosts["example.com/standing"]
		if !isHanded {
			test.Fatalf("switched %t, windows's build handed no side of example.com/standing", switched)
		}
		side, isSide := handed.(unused_code.BodyRecordHost)
		if !isSide {
			test.Fatalf("switched %t, the side windows's build handed is %T, which answers no body's record",
				switched, handed)
		}
		var kept []reachedBody
		for _, body := range engine.buildRuns[1].bodyReach.bodies {
			if body.name == "PlantedKept" {
				kept = append(kept, body)
			}
		}
		if len(kept) != 1 {
			test.Fatalf("switched %t, windows's census holds %d bodies named PlantedKept, want 1", switched,
				len(kept))
		}
		if held := side.RecordsHeld("example.com/standing"); held != switched {
			test.Errorf("switched %t, the side holds example.com/standing's records %t", switched, held)
		}
		body, digest, isRecorded := side.BodyRecord("example.com/standing", kept[0].file, kept[0].offset)
		if !switched {
			if isRecorded {
				test.Error("with the switch off the side answers PlantedKept's record")
			}
			continue
		}
		if !isRecorded {
			test.Fatalf("with the switch on the side answers no record of PlantedKept at %s offset %d",
				kept[0].file, kept[0].offset)
		}
		standing, readError := os.ReadFile(filepath.Join(root, "standing.go"))
		if readError != nil {
			test.Fatal(readError)
		}
		if file, offset := body.Key(); file != kept[0].file || offset != kept[0].offset ||
			digest != sha256.Sum256(standing) {
			test.Errorf("the side answers PlantedKept with the record keyed at %s offset %d and the digest %x, want "+
				"%s offset %d and standing.go's sha256", file, offset, digest, kept[0].file, kept[0].offset)
		}
	}
}
