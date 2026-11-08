package go_engine

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/ctrlflow"

	"github.com/system-inc/cohere/internal/go_engine/contract"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rules"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_abbreviated_identifier"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_uppercase_initialism"
	"github.com/system-inc/cohere/internal/go_rename/global_rename"
	"github.com/system-inc/cohere/internal/go_rename/rename"
	"github.com/system-inc/cohere/internal/go_rename/workspace"
)

// renamingPipeline is renamingPipelineOver the rename fixture.
func renamingPipeline(test *testing.T) (*pipeline, *load.Loaded) {
	test.Helper()
	return renamingPipelineOver(test, "rename")
}

// renamingPipelineOver is a pipeline over a fresh copy of a fixture, described as a run describes it, with the
// registry's rules, writing its records nowhere, and the copy's load from source.
func renamingPipelineOver(test *testing.T, fixture string) (*pipeline, *load.Loaded) {
	test.Helper()
	root := copyFixture(test, filepath.Join("testdata", fixture))
	request := invocation{root: root, switches: map[string]bool{}}
	described, describeError := describeProject(request)
	if describeError != nil {
		test.Fatal(describeError)
	}
	engine := &pipeline{
		request:       request,
		described:     described,
		registered:    rules.All(),
		writer:        contract.NewWriter(io.Discard),
		standardError: io.Discard,
	}
	loaded, loadError := load.Packages(described.workspace.Root, described.workspace.Patterns(),
		described.workspace.Environment(), described.readableOurs)
	if loadError != nil || len(loaded.Diagnostics) > 0 {
		test.Fatalf("loading the fixture: %v %+v", loadError, loaded.Diagnostics)
	}
	return engine, loaded
}

// planParseUrl is the plan to rename the fixture's ParseURL to ParseUrl, which renames it in four files.
func planParseUrl(test *testing.T, space *workspace.Workspace, loaded *load.Loaded) *global_rename.Result {
	test.Helper()
	var proposals []global_rename.Proposal
	for _, analyzed := range loaded.Packages {
		for identifier, object := range analyzed.TypesInfo.Defs {
			if object != nil && identifier.Name == "ParseURL" {
				proposals = append(proposals,
					global_rename.Proposal{Package: analyzed, Object: object, NewName: "ParseUrl"})
			}
		}
	}
	result := global_rename.Plan(space, proposals, global_rename.Options{HostOnly: true})
	if len(result.Accepted) != 1 || len(result.Edits) < 4 {
		test.Fatalf("want ParseURL's rename planned in four files, got %+v and %d edits", result.Accepted,
			len(result.Edits))
	}
	return result
}

// The proof is what stands between a rewrite and the disk: the plan's own rewrite of ParseURL proves, and a rewrite
// that differs from the plan is caught and names the rename, by its old and new name, with why. A use left unrenamed
// is a type error in the load of the rewrite, and a local renamed that the plan never asked for type-checks but reads
// back under another name than expected. Neither proof writes anything, so the tree on disk is as it was.
func TestAProofNamesTheRenamesItsRewriteBreaks(test *testing.T) {
	test.Parallel()

	for _, testCase := range []struct {
		name   string
		plant  func(test *testing.T, result *global_rename.Result, root string)
		reason string
	}{
		{
			name: "a use left unrenamed",
			plant: func(test *testing.T, result *global_rename.Result, root string) {
				external := filepath.Join(root, "internal", "store", "store_test.go")
				result.Edits = slices.DeleteFunc(result.Edits, func(edit rename.Edit) bool {
					return edit.Filename == external
				})
			},
			reason: "the rewrite does not type-check",
		},
		{
			// A comment edit is rewritten and never expected (see global_rename.Expect), so planting the local's
			// rename there makes a rewrite the plan's expectation does not hold.
			name: "a local the plan never renamed",
			plant: func(test *testing.T, result *global_rename.Result, root string) {
				file := filepath.Join(root, "internal", "store", "store.go")
				contents, readError := os.ReadFile(file)
				if readError != nil {
					test.Fatal(readError)
				}
				for _, around := range []string{"func Encode(", "json.Marshal("} {
					offset := strings.Index(string(contents), around+"record")
					if offset < 0 {
						test.Fatalf("store.go holds no %q", around+"record")
					}
					result.CommentEdits = append(result.CommentEdits, rename.Edit{
						Filename: file, Offset: offset + len(around), OldText: "record", NewText: "entry",
					})
				}
			},
			reason: "the rewrite reads back otherwise",
		},
	} {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()

			engine, loaded := renamingPipeline(test)
			root := engine.described.workspace.Root
			before := map[string][]byte{}
			for _, file := range engine.described.readableOurs {
				contents, readError := os.ReadFile(file)
				if readError != nil {
					test.Fatal(readError)
				}
				before[file] = contents
			}
			space := workspace.FromLoaded(root, loaded.Roots, loaded.Packages, nil)
			result := planParseUrl(test, space, loaded)
			proved := engine.prove(space, map[string][]byte{}, result, global_rename.Options{})
			if proved.proof == nil || len(proved.failures) > 0 {
				test.Fatalf("the plan's own rewrite failed its proof: %v", proved.failures)
			}

			testCase.plant(test, result, root)
			failures := engine.prove(space, map[string][]byte{}, result, global_rename.Options{}).failures
			reason, failed := failures[[2]string{"ParseURL", "ParseUrl"}]
			if !failed || !strings.Contains(reason, testCase.reason) {
				test.Fatalf("want ParseURL's rename failed as %q, got %v", testCase.reason, failures)
			}
			for file, contents := range before {
				after, readError := os.ReadFile(file)
				if readError != nil || string(after) != string(contents) {
					test.Errorf("a proof changed %s on disk (%v)", file, readError)
				}
			}
		})
	}
}

// A rename that reaches a file only windows builds is proven under windows through the engine's own load of it, so a
// rewrite that leaves that file naming the old name fails there, naming the rename and the platform, though the
// host's proof, which never reads the file, holds. The plan's own rewrite proves under windows and freebsd both.
func TestAProofUnderAPlatformNamesTheRenameItsRewriteBreaks(test *testing.T) {
	test.Parallel()

	engine, loaded := renamingPipelineOver(test, "rename-platforms")
	root := engine.described.workspace.Root
	space := workspace.FromLoaded(root, loaded.Roots, loaded.Packages, nil)
	options := global_rename.Options{LoadPlatform: engine.platformLoad(space, nil), RequireSoundPlatforms: true}
	var proposals []global_rename.Proposal
	for _, analyzed := range loaded.Packages {
		for identifier, object := range analyzed.TypesInfo.Defs {
			if object != nil && identifier.Name == "maxBytes" {
				proposals = append(proposals,
					global_rename.Proposal{Package: analyzed, Object: object, NewName: "maximumBytes"})
			}
		}
	}
	result := global_rename.Plan(space, proposals, options)
	platforms := []string{}
	for _, platform := range result.Platforms() {
		platforms = append(platforms, platform.String())
	}
	if len(result.Accepted) != 1 || len(result.TwinEdits) != 2 || !slices.Equal(platforms,
		[]string{"windows/amd64", "freebsd/amd64"}) {
		test.Fatalf("want maxBytes renamed in both platforms' files and read under both, got %+v, %d twin edits, %v",
			result.Accepted, len(result.TwinEdits), platforms)
	}
	if proved := engine.prove(space, map[string][]byte{}, result, options); proved.proof == nil ||
		len(proved.failures) > 0 {
		test.Fatalf("the plan's own rewrite failed its proof: %v", proved.failures)
	}

	windowsFile := filepath.Join(root, "internal", "limits", "limits_windows.go")
	result.TwinEdits = slices.DeleteFunc(result.TwinEdits, func(edit rename.Edit) bool {
		return edit.Filename == windowsFile
	})
	failures := engine.prove(space, map[string][]byte{}, result, options).failures
	reason, failed := failures[[2]string{"maxBytes", "maximumBytes"}]
	if !failed || !strings.Contains(reason, "windows/amd64") {
		test.Fatalf("want maxBytes's rename failed under windows/amd64, got %v", failures)
	}
}

// A platform's proof requires every identifier of a file only it builds to resolve to the declaration it did, not only
// that the platform still type-checks it. The rewrite here renames maxBytes in limits.go and, as a twin edit, in the
// file only windows builds, where a local already holds the new name: the use becomes the local's, windows still
// type-checks it, and the host never reads it. The plan's own scope check refuses this rename (the rename-capture
// case), so the rewrite is made by hand, to hold the proof to its half that compares resolutions.
func TestAPlatformProofRefusesARenameThatCapturesAUse(test *testing.T) {
	test.Parallel()

	engine, loaded := renamingPipelineOver(test, "rename-capture")
	root := engine.described.workspace.Root
	space := workspace.FromLoaded(root, loaded.Roots, loaded.Packages, nil)
	options := global_rename.Options{LoadPlatform: engine.platformLoad(space, nil), RequireSoundPlatforms: true}
	edits := func(relative string, from int, every bool) []rename.Edit {
		file := filepath.Join(root, filepath.FromSlash(relative))
		contents, readError := os.ReadFile(file)
		if readError != nil {
			test.Fatal(readError)
		}
		var found []rename.Edit
		for offset := from; ; {
			index := strings.Index(string(contents[offset:]), "maxBytes")
			if index < 0 {
				break
			}
			found = append(found, rename.Edit{Filename: file, Offset: offset + index, OldText: "maxBytes",
				NewText: "maximumBytes"})
			offset += index + len("maxBytes")
			if !every {
				break
			}
		}
		return found
	}
	// limits.go's doc comment and the declaration's own spell maxBytes first, so its identifiers start at the const.
	limitsSource, readError := os.ReadFile(filepath.Join(root, "internal", "limits", "limits.go"))
	if readError != nil {
		test.Fatal(readError)
	}
	result := &global_rename.Result{
		Edits:     edits("internal/limits/limits.go", strings.Index(string(limitsSource), "const maxBytes"), true),
		TwinEdits: edits("internal/limits/limits_windows.go", 0, false),
	}
	if len(result.Edits) != 2 || len(result.TwinEdits) != 1 {
		test.Fatalf("want the declaration and its use in limits.go and the use under windows, got %+v and %+v",
			result.Edits, result.TwinEdits)
	}
	rewritten, rewriteError := global_rename.RewrittenFiles(result)
	if rewriteError != nil {
		test.Fatal(rewriteError)
	}
	windows := workspace.Platform{OperatingSystem: "windows", Architecture: "amd64"}
	verification := global_rename.ProvePlatform(space, global_rename.Expect(space, result), windows, rewritten,
		options)
	captured := slices.ContainsFunc(verification.Mismatches, func(mismatch global_rename.Mismatch) bool {
		return mismatch.Kind == global_rename.MismatchKindIdentifier && mismatch.Platform == "windows/amd64" &&
			strings.HasSuffix(mismatch.Filename, "limits_windows.go")
	})
	if !captured {
		test.Fatalf("want the captured use in limits_windows.go a mismatch under windows/amd64, got %+v",
			verification.Mismatches)
	}
	for _, mismatch := range verification.Mismatches {
		if mismatch.Kind == global_rename.MismatchKindPlatformPackage || mismatch.Kind ==
			global_rename.MismatchKindPlatformLoad {
			test.Fatalf("want windows to type-check the captured rewrite, got %s", mismatch)
		}
	}
}

// A naming rule that learns facts only through what it requires gets one round, never a second planned over roots
// whose types its run let go of. On Nov 8 a syntax listener required staticcheck's purity, so the syntax dispatcher
// did, and so both naming rules did. Their run over the first round's proof analyzed every dependency for purity's
// facts and let go of plain store's syntax and types, store being a root of the load beside "store [store.test]" and a
// dependency of every package importing it, and the second round's plan read store with no types (#dtyzaf7). Here
// both naming rules require ctrlflow, which learns facts: the run renames, says why there is no second round, and ends
// as a run does.
func TestANamingRuleLearningFactsThroughItsRequirementsGetsOneRound(test *testing.T) {
	test.Parallel()

	learners := map[string]*analysis.Analyzer{
		consistency_no_abbreviated_identifier.Name: consistency_no_abbreviated_identifier.NewAnalyzer(),
		consistency_no_uppercase_initialism.Name:   consistency_no_uppercase_initialism.NewAnalyzer(),
	}
	registered := slices.Clone(rules.All())
	for index, each := range registered {
		if learner, naming := learners[each.Name]; naming {
			learner.Requires = append(learner.Requires, ctrlflow.Analyzer)
			registered[index].Analyzer = learner
		}
	}
	root := copyFixture(test, filepath.Join("testdata", "rename"))
	var standardOutput bytes.Buffer
	var standardError bytes.Buffer
	exitCode := run([]string{"--contract", fmt.Sprint(contract.Version), "--root", root, "--no-cache"},
		&standardOutput, &standardError, registered, nil)
	if exitCode != exitClean && exitCode != exitFindings {
		test.Fatalf("exit %d, want %d or %d; stderr:\n%s", exitCode, exitClean, exitFindings, standardError.String())
	}
	var record contract.FixRecord
	for line := range strings.Lines(standardOutput.String()) {
		if strings.HasPrefix(line, `{"kind":"fix"`) {
			if decodeError := json.Unmarshal([]byte(line), &record); decodeError != nil {
				test.Fatalf("reading the fix record %s: %v", line, decodeError)
			}
		}
	}
	if record.FilesRewritten == 0 {
		test.Fatalf("want the first round to rename, got\n%s\n%s", standardOutput.String(), standardError.String())
	}
	note := consistency_no_abbreviated_identifier.Name + " learns facts through ctrlflow, so its renames are not " +
		"asked for again after a round"
	if !strings.Contains(standardError.String(), note) {
		test.Fatalf("want stderr to say %q, got:\n%s", note, standardError.String())
	}
}
