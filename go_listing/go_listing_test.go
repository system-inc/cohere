package go_listing_test

import (
	"slices"
	"testing"

	"github.com/system-inc/cohere/go_listing"
)

// Each mutant below must make a test fail:
//
//	Reach without the root's test main: TestReachIsWhatABuildOfTheRootReads
//	Reach without its guard on the empty root: TestDecodeReadsAnObjectOfAFieldList
//	CgoIncludeDirectories reading "-I" "<directory>" as no directory: TestReachIsWhatABuildOfTheRootReads
//	SourceFiles answering EmbedFiles with GoFiles: TestSourceFilesNamesExactlyTheFixturesFiles
//	DepsErrors decoded under another tag: TestDecodeReadsTheGoldenAsGoPrintedIt
//	ReachError passing over DepsErrors: TestDecodeReadsAnObjectOfAFieldList

// Decode reads what go list prints and refuses anything else, never a part of it.
func TestDecodeReadsAnObjectOfAFieldList(test *testing.T) {
	test.Parallel()
	for name, output := range map[string]string{
		"an object cut short":     "{\"ImportPath\": \"a\"}\n{\"ImportPath\": ",
		"text":                    "can't load package: no Go files\n",
		"a null":                  "{\"ImportPath\": \"a\"}\nnull\n",
		"an array":                "[{\"ImportPath\": \"a\"}]",
		"a field of another type": "{\"GoFiles\": \"a.go\"}",
	} {
		if packages, decodeError := go_listing.Decode([]byte(output)); decodeError == nil {
			test.Errorf("%s: read as %+v", name, packages)
		}
	}
	// A field list that leaves out ImportPath, as build_inputs' does, is read as it stands.
	packages, decodeError := go_listing.Decode([]byte(
		"{\n\t\"Dir\": \"/a\",\n\t\"GoFiles\": [\n\t\t\"a.go\"\n\t]\n}\n\n  {\"Dir\": \"/b\", \"Standard\": true}\n",
	))
	if decodeError != nil ||
		len(packages) != 2 ||
		packages[0].Directory != "/a" ||
		!slices.Equal(packages[0].GoFiles, []string{"a.go"}) ||
		!packages[1].Standard {
		test.Errorf("a field list's objects read as %+v (%v)", packages, decodeError)
	}
	if packages, decodeError := go_listing.Decode(nil); decodeError != nil || len(packages) != 0 {
		test.Errorf("the empty output reads as %+v (%v)", packages, decodeError)
	}
	// The empty root reaches nothing, though these objects, listed without ImportPath, have its empty one.
	if reach := go_listing.Reach(packages, ""); len(reach) != 0 {
		test.Errorf("the empty root reaches %+v", reach)
	}
	// A reach's DepsErrors refuse it with no package's own Error in it.
	if reachError := go_listing.ReachError([]go_listing.Package{{
		ImportPath: "a",
		DepsErrors: []*go_listing.PackageError{{Message: "no Go files"}},
	}}); reachError == nil || reachError.Error() != "a, through a dependency: no Go files" {
		test.Errorf("a dependency's error refuses the reach as %v", reachError)
	}
}

// CgoIncludeDirectories reads -I in either spelling from all three flag lists, places each against the
// package's directory, and names each once.
func TestCgoIncludeDirectoriesReadsEitherSpelling(test *testing.T) {
	test.Parallel()
	listed := go_listing.Package{
		Directory:   "/module/package",
		CgoCFlags:   []string{"-O2", "-I", "include", "-I/absolute/./one", "-DNAME=-Inot"},
		CgoCppFlags: []string{"-I../shared", "-Iinclude", "-isystem", "/system"},
		CgoCxxFlags: []string{"-I/absolute/one", "-I"},
	}
	if got, want := listed.CgoIncludeDirectories(), []string{
		"/module/package/include",
		"/absolute/one",
		"/module/shared",
	}; !slices.Equal(got, want) {
		test.Errorf("the include directories are %q, want %q", got, want)
	}
}

// SourceFiles has no default: no lists is no files, and a list no constant names panics rather than
// reading as an empty one.
func TestSourceFilesHasNoDefault(test *testing.T) {
	test.Parallel()
	listed := go_listing.Package{GoFiles: []string{"a.go"}, EmbedFiles: []string{"a.txt"}}
	if files := listed.SourceFiles(nil); len(files) != 0 {
		test.Errorf("no lists names %q", files)
	}
	if files := listed.SourceFiles([]go_listing.FileList{go_listing.EmbedFiles, go_listing.GoFiles}); !slices.Equal(
		files,
		[]string{"a.txt", "a.go"},
	) {
		test.Errorf("the lists in the caller's order name %q", files)
	}
	defer func() {
		if recover() == nil {
			test.Error("a list no constant names read as an empty one")
		}
	}()
	listed.SourceFiles([]go_listing.FileList{"Gofiles"})
}

// Arguments spells each flag by name in one order, and refuses what go would read as something else.
func TestArgumentsSpellsEachFlag(test *testing.T) {
	test.Parallel()
	arguments, argumentsError := go_listing.Arguments(go_listing.Request{
		Patterns:              []string{"./...", "std"},
		BuildFlags:            []string{"-tags", "a,b", "-mod=readonly"},
		Fields:                []string{"ImportPath", "GoFiles"},
		WithoutVersionControl: true,
		Export:                true,
		Compiled:              true,
		Tests:                 true,
		Dependencies:          true,
		Errors:                true,
	})
	if want := []string{
		"list", "-e", "-deps", "-test", "-compiled", "-export", "-buildvcs=false",
		"-json=ImportPath,GoFiles", "-tags", "a,b", "-mod=readonly", "./...", "std",
	}; argumentsError != nil || !slices.Equal(arguments, want) {
		test.Errorf("every flag is %q (%v), want %q", arguments, argumentsError, want)
	}
	arguments, argumentsError = go_listing.Arguments(go_listing.Request{})
	if argumentsError != nil || !slices.Equal(arguments, []string{"list", "-json"}) {
		test.Errorf("no flags is %q (%v)", arguments, argumentsError)
	}
	for name, request := range map[string]go_listing.Request{
		"a pattern like a flag": {Patterns: []string{"-x"}},
		"a field with a comma":  {Fields: []string{"ImportPath,Dir"}},
		"an empty field":        {Fields: []string{""}},
	} {
		if arguments, argumentsError := go_listing.Arguments(request); argumentsError == nil {
			test.Errorf("%s: spelled %q", name, arguments)
		}
	}
}
