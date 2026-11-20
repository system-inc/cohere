package load

import (
	"maps"
	"reflect"
	"slices"
	"testing"
)

// differingModuleFiles are targetedModuleFiles with everything else a build's listing may hold that the host's does
// not, so a listing that took anything of another build's shows it: a module of its own below the root, which the
// root's walk passes by and only the windows file imports, so only the windows build reaches it, a directory that is
// a package under windows and none under any other target, and a package with a cgo file, which a build without cgo
// leaves out with its import.
func differingModuleFiles() map[string]string {
	files := maps.Clone(targetedModuleFiles)
	files["go.mod"] = "module example.com/targeted\n\ngo 1.27\n\nrequire example.com/reached v0.0.0\n\n" +
		"replace example.com/reached => ./reached\n"
	files["targeted_windows.go"] = "package targeted\n\nimport (\n\t_ \"container/list\"\n\n" +
		"\t_ \"example.com/reached\"\n)\n"
	files["reached/go.mod"] = "module example.com/reached\n\ngo 1.27\n"
	files["reached/reached.go"] = "// Package reached is a module only the windows build reaches.\npackage reached\n"
	files["onlywindows/only_windows.go"] = "// Package onlywindows is a package under windows alone.\n" +
		"package onlywindows\n"
	files["withcgo/withcgo.go"] = "// Package withcgo holds a file only a build with cgo compiles.\npackage withcgo\n"
	files["withcgo/translated.go"] = "package withcgo\n\n// int translated(void) { return 1; }\nimport \"C\"\n\n" +
		"// Translated is what the C function returns.\nfunc Translated() int { return int(C.translated()) }\n"
	return files
}

// importPathsOf are the import paths of the packages listed holds that the standard library does not, sorted.
func importPathsOf(listed *listing) []string {
	var paths []string
	for _, listedPackage := range listed.packages {
		if listedPackage.Module != nil {
			paths = append(paths, listedPackage.ImportPath)
		}
	}
	slices.Sort(paths)
	return paths
}

// Each build's listing through the walks the host's listing walked is the listing it makes walking for itself,
// package for package, field for field and in the same order (#vnb7btx): a walk reads directory names and the
// presence of a go.mod, and is keyed by its root, its module and its pattern alone, so the host's walk is every
// build's, and each build still decides for itself which of the walked directories hold a package of its own. The
// host lists first, as in a run, so the one walk is the host's, over a module where the builds differ in every way a
// listing can (differingModuleFiles): no build's listing is the host's, only the windows build lists the module and
// the directory that are its alone, and only a build with cgo lists the cgo file.
func TestEachBuildsListingThroughTheHostsWalksIsItsOwn(test *testing.T) {
	test.Parallel()
	directory := test.TempDir()
	writeListingFiles(test, directory, differingModuleFiles())
	patterns := []string{"./..."}
	walks := NewPackageWalks()
	shared := NewSharedReads()
	environmentOf := func(target []string) *GoEnvironment {
		return goEnvironmentOf(test, directory, append([]string{"GOWORK=off"}, target...))
	}
	host, hostError := list(environmentOf(sharedListingTargets[0]), patterns, nil, shared, walks, nil)
	if hostError != nil {
		test.Fatalf("the host's listing: %v", hostError)
	}
	if !host.inProcess {
		test.Fatalf("go list answered the host's listing, since %s, so nothing was walked", host.goListReason)
	}
	windowsAlone := []string{"example.com/reached", "example.com/targeted/onlywindows"}
	for _, target := range sharedListingTargets[1:] {
		alone, aloneError := list(environmentOf(target), patterns, nil, nil, nil, nil)
		through, throughError := list(environmentOf(target), patterns, nil, shared, walks, nil)
		if aloneError != nil || throughError != nil {
			test.Fatalf("%v: listed alone with %v, through the host's walks with %v", target, aloneError, throughError)
		}
		if !through.inProcess {
			test.Fatalf("%v: go list answered, since %s, so no walk was taken", target, through.goListReason)
		}
		if !reflect.DeepEqual(through, alone) {
			test.Errorf("%v: the listing through the host's walks differs from its own:\n%s", target,
				listingDifferences(through, alone))
		}
		if reflect.DeepEqual(through.packages, host.packages) {
			test.Errorf("%v: the listing is the host's, package for package, so the fixture tells no build apart",
				target)
		}
		// The fixture's own differences, read from the listing through the walks: the module and the directory that
		// are windows' alone, and the cgo file a build without cgo never lists.
		paths := importPathsOf(through)
		for _, path := range windowsAlone {
			if listsIt, windows := slices.Contains(paths, path), slices.Contains(target, "GOOS=windows"); listsIt !=
				windows {
				test.Errorf("%v: the listing holds %s is %t, want it under windows alone, of %q", target, path, listsIt,
					paths)
			}
		}
		withCgo := through.byPath["example.com/targeted/withcgo"]
		if withCgo == nil {
			test.Fatalf("%v: the listing holds no example.com/targeted/withcgo, of %q", target, paths)
		}
		if translated, cgo := len(withCgo.CgoFiles) == 1, slices.Contains(target, "CGO_ENABLED=1"); translated != cgo {
			test.Errorf("%v: the cgo package lists the cgo files %q, want its one under cgo alone", target,
				withCgo.CgoFiles)
		}
	}
	for _, path := range windowsAlone {
		if slices.Contains(importPathsOf(host), path) {
			test.Errorf("the host's listing holds %s, which is windows' alone", path)
		}
	}
	// The module's one pattern was walked once, by the host, and asked for by every listing through the walks. The
	// module below the root is no pattern's: the windows build reaches it by an import, and nothing walks it.
	patternsWalked, walked, asked := walks.Walked()
	if patternsWalked != 1 || walked != 1 || asked != int64(len(sharedListingTargets)) {
		test.Errorf("the walks hold %d patterns, walked %d times and asked %d, want the one pattern, walked once by "+
			"the host and asked by each of the %d listings", patternsWalked, walked, asked, len(sharedListingTargets))
	}
}
