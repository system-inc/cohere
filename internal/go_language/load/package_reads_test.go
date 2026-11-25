package load

import (
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// packageReadTargets are the builds that take the host's reads in the differential, each an environment over the
// host's: four ports, each with cgo off and on, and two with the race tag, the host's own port among them, whose
// only difference from the host's build is the tag.
var packageReadTargets = [][]string{
	{"GOOS=linux", "GOARCH=amd64", "CGO_ENABLED=0", "GOFLAGS="},
	{"GOOS=linux", "GOARCH=amd64", "CGO_ENABLED=1", "GOFLAGS="},
	{"GOOS=windows", "GOARCH=amd64", "CGO_ENABLED=0", "GOFLAGS="},
	{"GOOS=windows", "GOARCH=amd64", "CGO_ENABLED=1", "GOFLAGS="},
	{"GOOS=freebsd", "GOARCH=arm64", "CGO_ENABLED=0", "GOFLAGS="},
	{"GOOS=freebsd", "GOARCH=arm64", "CGO_ENABLED=1", "GOFLAGS="},
	{"GOOS=darwin", "GOARCH=amd64", "CGO_ENABLED=0", "GOFLAGS="},
	{"GOOS=darwin", "GOARCH=amd64", "CGO_ENABLED=1", "GOFLAGS="},
	{"GOOS=linux", "GOARCH=amd64", "CGO_ENABLED=0", "GOFLAGS=-tags=race"},
	{"CGO_ENABLED=1", "GOFLAGS=-tags=race"},
}

// packageReadFiles are directories the differential reads beside the toolchain's and cohere's: a cgo file whose `#cgo`
// lines differ by operating system, which go/build matches with no tag recorded, cgo on or off, so its directory's
// tags alone would take one system's flags for another's.
var packageReadFiles = map[string]string{
	"cgo/cgo.go": "package cgo\n\n// #cgo linux LDFLAGS: -lm\n// #cgo darwin LDFLAGS: -lz\n" +
		"// int one(void) { return 1; }\nimport \"C\"\n\n// One is one.\nfunc One() int { return int(C.one()) }\n",
}

// A build that takes the host's read of a directory, where it selects every file as the host's build did, reads the
// Package and the error go/build's own ImportDir reads under it, field for field (#y8cf1wf): every directory of the
// toolchain's two modules and of cohere's, as go's walk finds them (packageDirectories), testdata left out, under
// four ports, cgo off and on, and a tag. The host reads every directory first, with cgo on, and each build after it
// takes what it can from the reads kept before it, so each takes some and reads some itself, or the test proves
// nothing.
func TestATakenReadIsGoBuildsOwn(test *testing.T) {
	test.Parallel()
	root, absoluteError := filepath.Abs(filepath.Join("..", "..", ".."))
	if absoluteError != nil {
		test.Fatal(absoluteError)
	}
	fixture := test.TempDir()
	writeListingFiles(test, fixture, packageReadFiles)
	hostEnvironment := goEnvironmentOf(test, root, []string{"CGO_ENABLED=1", "GOFLAGS="})
	shared := NewSharedReads()
	host, doubt := newInProcessLister(hostEnvironment, nil, shared, nil)
	if doubt != "" {
		test.Fatalf("no host lister: %s", doubt)
	}
	directories := []string{filepath.Join(fixture, "cgo")}
	// The toolchain's two modules, the standard library's and the commands', and cohere's.
	source := filepath.Join(host.context.GOROOT, "src")
	for _, each := range []string{source, filepath.Join(source, "cmd"), root} {
		walked, whole := packageDirectories(each)
		if !whole {
			test.Fatalf("%s was not walked whole", each)
		}
		directories = append(directories, walked...)
	}
	readEvery(host, directories)
	for _, target := range packageReadTargets {
		goEnvironment := goEnvironmentOf(test, root, target)
		lister, doubt := newInProcessLister(goEnvironment, nil, shared, nil)
		if doubt != "" {
			test.Fatalf("%v: no lister: %s", target, doubt)
		}
		readEvery(lister, directories)
		var group sync.WaitGroup
		for _, directory := range directories {
			group.Go(func() { requireGoBuildsRead(test, lister, directory, strings.Join(target, " ")) })
		}
		group.Wait()
		taken, read := lister.directoriesTaken.Load(), lister.directoriesRead.Load()
		test.Logf("%v: %d directories taken, %d read again", target, taken, read)
		if taken == 0 || read == 0 {
			test.Errorf("%v: %d directories taken and %d read again, so one way was never compared", target, taken,
				read)
		}
	}
}

// readEvery reads every one of directories through lister, all at once, as a listing's walk and imports read them.
func readEvery(lister *inProcessLister, directories []string) {
	var group sync.WaitGroup
	for _, directory := range directories {
		group.Go(func() { lister.readDirectory(directory) })
	}
	group.Wait()
}
