package go_engine

import (
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/contract"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rules"
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
