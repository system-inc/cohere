package load

import (
	"fmt"
	"maps"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	copiedbuild "github.com/system-inc/cohere/internal/go_engine/load/build"
)

// headerModuleFiles are a module whose directories read every way go/build reads a header: build constraints written
// only as `// +build` lines, as both kinds where they disagree and `//go:build` decides, and as two `//go:build` lines,
// which is an error; a header longer than a shared file head, and a `//go:embed` line past one; a byte order mark; a
// header that does not parse, read whole; a binary-only package; a documentation file; tests of both kinds; a custom
// tag; and a cgo file, whose directory go/build reads itself.
var headerModuleFiles = map[string]string{
	"go.mod":           "module example.com/headers\n\ngo 1.27\n",
	"plain.go":         "// Package headers is read.\npackage headers\n\nimport \"os\"\n\nvar _ = os.Args\n",
	"plus_build.go":    "// +build linux darwin\n\npackage headers\n\nimport _ \"container/list\"\n",
	"plus_negated.go":  "// +build !windows,!freebsd\n\npackage headers\n\nimport _ \"container/ring\"\n",
	"both.go":          "//go:build windows\n// +build windows\n\npackage headers\n\nimport _ \"container/heap\"\n",
	"disagree.go":      "//go:build linux\n// +build windows\n\npackage headers\n\nimport _ \"hash/adler32\"\n",
	"tagged.go":        "//go:build cohere_listing_tag\n\npackage headers\n\nimport _ \"hash/crc32\"\n",
	"documentation.go": "package documentation\n",
	"plain_test.go":    "package headers\n\nimport \"testing\"\n\nfunc TestPlain(test *testing.T) {}\n",
	"external_test.go": "package headers_test\n\nimport (\n\t\"testing\"\n\n\t_ \"example.com/headers\"\n)\n\n" +
		"func TestExternal(test *testing.T) {}\n",
	"twice/twice.go": "//go:build linux\n//go:build darwin\n\npackage twice\n",
	"long/long.go": "// " + strings.Repeat("x", 2*sharedFileHead) + "\n\npackage long\n\nimport \"os\"\n\n" +
		"var _ = os.Args\n",
	"embedded/embedded.go": "package embedded\n\nimport _ \"embed\"\n\n// " + strings.Repeat("y", 2*sharedFileHead) +
		"\n\n//go:embed data.txt\nvar data string\n",
	"embedded/data.txt": "data\n",
	"bom/bom.go":        "\ufeffpackage bom\n\nimport \"os\"\n\nvar _ = os.Args\n",
	"syntax/bad.go":     "package bad\n\nimport (\n\t\"os\"\n",
	"binary/binary.go":  "//go:binary-only-package\n\npackage binary\n",
	"cgo/cgo.go": "package cgo\n\n// #cgo linux LDFLAGS: -lm\n// int one(void) { return 1; }\nimport \"C\"\n\n" +
		"// One is one.\nfunc One() int { return int(C.one()) }\n",
}

// headerModuleDirectories are headerModuleFiles' directories, the module's own first.
var headerModuleDirectories = []string{".", "twice", "long", "embedded", "bom", "syntax", "binary", "cgo"}

// Every directory every build reads through the copy of go/build's directory read is the Package and the error
// go/build's own ImportDir reads under that build's context, field for field, though each Go file's header is read
// once for every build (#tb538na): each build matches the files by its own name rules, constraints and cgo setting.
// One build's file is held, so its header is read from the held bytes, which differ from the disk's.
func TestEveryDirectoryReadsAsGoBuildReadsIt(test *testing.T) {
	test.Parallel()
	directory := test.TempDir()
	writeListingFiles(test, directory, headerModuleFiles)
	plain := filepath.Join(directory, "plain.go")
	heldPlain := []byte("// Package headers is held.\npackage headers\n\nimport \"strings\"\n\nvar _ = strings.Cut\n")
	held := Held(func(file string) ([]byte, bool) { return heldPlain, file == plain })
	shared := NewSharedReads()
	if shared.headers == nil {
		test.Fatalf("the shared reads share no header: %s", shared.HeadersUnshared())
	}
	reads := 0
	for _, target := range sharedListingTargets {
		goEnvironment := goEnvironmentOf(test, directory, append([]string{"GOWORK=off"}, target...))
		lister, doubt := newInProcessLister(goEnvironment, held, shared, nil)
		if doubt != "" {
			test.Fatalf("%v: no lister: %s", target, doubt)
		}
		for _, each := range headerModuleDirectories {
			reads++
			requireGoBuildsRead(test, lister, filepath.Join(directory, each), fmt.Sprint(target))
		}
	}
	// Every Go file is read once, the held one included, however many builds read it: without the shared reads each
	// build would read every file its names admit.
	goFiles := 0
	for name := range headerModuleFiles {
		if strings.HasSuffix(name, ".go") {
			goFiles++
		}
	}
	if parsed := shared.headers.Parsed(); parsed == 0 || parsed > int64(goFiles) {
		test.Errorf("%d directory reads read %d headers, want each of the %d Go files' once at most", reads, parsed,
			goFiles)
	}
}

// The same holds at cohere's own root, under every build the engine checks it with, the host's first, each listing
// the whole workspace through the one run's shared reads, all at once, as the extra builds list side by side.
func TestEveryDirectoryReadsAsGoBuildReadsItOnCoheresBuilds(test *testing.T) {
	test.Parallel()
	workspace, builds := coheresBuilds(test)
	shared := NewSharedReads()
	listers := make([]*inProcessLister, len(builds))
	doubts := make([]string, len(builds))
	for index, each := range builds {
		goEnvironment := goEnvironmentOf(test, workspace.Root, append(workspace.Environment(), each.Environment()...))
		listers[index], doubts[index] = newInProcessLister(goEnvironment, nil, shared, nil)
		if doubts[index] != "" {
			test.Fatalf("%s: no lister: %s", each, doubts[index])
		}
	}
	var group sync.WaitGroup
	for index := range builds {
		group.Go(func() { _, doubts[index] = listers[index].list(workspace.Patterns()) })
	}
	group.Wait()
	for index, each := range builds {
		if doubts[index] != "" {
			test.Fatalf("%s: the lister did not answer: %s", each, doubts[index])
		}
		for _, directory := range slices.Sorted(maps.Keys(listers[index].reads)) {
			requireGoBuildsRead(test, listers[index], directory, each.String())
		}
	}
	if shared.headers.Parsed() == 0 {
		test.Error("no header was read through the shared reads, so nothing was compared")
	}
}

// requireGoBuildsRead fails unless lister's read of directory is go/build's own under lister's build context.
func requireGoBuildsRead(test *testing.T, lister *inProcessLister, directory string, build string) {
	test.Helper()
	read := lister.readDirectory(directory)
	built, importError := lister.context.ImportDir(directory, 0)
	if !reflect.DeepEqual(read.built, built) {
		test.Errorf("%s: %s reads\n%+v\nwhere go/build reads\n%+v", build, directory, read.built, built)
	}
	if fmt.Sprintf("%T %v", read.importError, read.importError) != fmt.Sprintf("%T %v", importError, importError) {
		test.Errorf("%s: %s reads with %T %v where go/build reads with %T %v", build, directory, read.importError,
			read.importError, importError, importError)
	}
}

// A held file the run writes between two listings, as the long-line wrap writes between the host's load and the
// extra builds', is read again, and the second listing reads it as written, never the first read's header.
func TestAHeldFileWrittenBetweenListingsIsReadAgain(test *testing.T) {
	test.Parallel()
	directory := test.TempDir()
	writeListingFiles(test, directory, headerModuleFiles)
	plain := filepath.Join(directory, "plain.go")
	shared := NewSharedReads()
	// Two imports no other file of the module makes, so the directory's imports say which bytes were read.
	versions := []string{"text/scanner", "text/template/parse"}
	for index, written := range versions {
		contents := []byte("package headers\n\nimport _ \"" + written + "\"\n")
		held := Held(func(file string) ([]byte, bool) { return contents, file == plain })
		goEnvironment := goEnvironmentOf(test, directory, append([]string{"GOWORK=off"}, sharedListingTargets[0]...))
		lister, doubt := newInProcessLister(goEnvironment, held, shared, nil)
		if doubt != "" {
			test.Fatalf("no lister: %s", doubt)
		}
		read := lister.readDirectory(directory)
		if read.importError != nil || !slices.Contains(read.built.Imports, written) ||
			index > 0 && slices.Contains(read.built.Imports, versions[0]) {
			test.Errorf("with plain.go importing %s, the directory reads imports %v with %v", written,
				read.built.Imports, read.importError)
		}
	}
}

// A binary linked by another Go release than the copy's reads every directory through go/build, and the shared reads
// say why, naming both releases, which the engine writes in its types record. The listing is the one the copy reads.
func TestAnotherGoReleaseReadsThroughGoBuildAndSaysSo(test *testing.T) {
	test.Parallel()
	directory := test.TempDir()
	writeListingFiles(test, directory, headerModuleFiles)
	goEnvironment := goEnvironmentOf(test, directory, append([]string{"GOWORK=off"}, sharedListingTargets[1]...))
	patterns := []string{"./bom", "./long", "./embedded"}
	another := newSharedReads("go1.28.0")
	if another.headers != nil {
		test.Error("a binary of go1.28.0 shares headers through the copy of go1.27.0's go/build")
	}
	unshared := another.HeadersUnshared()
	if !strings.Contains(unshared, "go1.28.0") || !strings.Contains(unshared, copiedbuild.CopiedToolchain) {
		test.Errorf("the shared reads say %q, naming not both releases", unshared)
	}
	throughGoBuild, doubt := listInProcess(goEnvironment, patterns, nil, another, nil, nil)
	if doubt != "" {
		test.Fatalf("the lister did not answer through go/build: %s", doubt)
	}
	copied := newSharedReads(copiedbuild.CopiedToolchain + " X:nodwarf5")
	if copied.headers == nil || copied.HeadersUnshared() != "" {
		test.Fatalf("a binary of the copy's release, an experiment on, shares no header: %s", copied.HeadersUnshared())
	}
	throughCopy, doubt := listInProcess(goEnvironment, patterns, nil, copied, nil, nil)
	if doubt != "" {
		test.Fatalf("the lister did not answer through the copy: %s", doubt)
	}
	if copied.headers.Parsed() == 0 {
		test.Error("the copy read no header")
	}
	if !reflect.DeepEqual(throughCopy, throughGoBuild) {
		test.Errorf("the listing through the copy differs from go/build's:\n%s",
			listingDifferences(throughCopy, throughGoBuild))
	}
	var nothing *SharedReads
	if nothing.HeadersUnshared() != "" {
		test.Error("no shared reads say headers were unshared")
	}
}
