package load

import (
	"slices"
	"testing"
)

// A listing the run remembers answers a later ask only while every module file a go command of the run can rewrite
// reads as it did (#32594zm). With no store, as under `--no-cache`, a build lists, asks again through the same
// fingerprints and is answered with the listing it made; then the file is rewritten, standing in for another build's
// go list, and the next ask lists again, and the listing it makes is the one remembered after it. It holds for
// go.work.sum in a go.work root, and for go.mod under -mod=mod.
func TestARememberedListingIsRefusedOnceARewritableModuleFileMoved(test *testing.T) {
	test.Parallel()
	for _, rewritten := range rewrittenModuleFiles {
		test.Run(rewritten.name, func(test *testing.T) {
			test.Parallel()
			directory := test.TempDir()
			writeListingFiles(test, directory, targetedModuleFiles)
			writeListingFiles(test, directory, rewritten.files)
			linux := goEnvironmentOf(test, directory, append(slices.Clone(sharedListingTargets[1]),
				rewritten.environment...))
			fingerprints := NewFingerprints()
			// ask lists linux's patterns through fingerprints with no store, as a build of a run without one does.
			ask := func() *listing {
				listed, kept, listError := cachedList(linux, []string{"./..."}, nil, nil, NewSharedReads(),
					fingerprints, nil, nil)
				if listError != nil || kept {
					test.Fatalf("listed kept %v with %v, want a listing and nothing kept", kept, listError)
				}
				return listed
			}
			first := ask()
			if second := ask(); second != first {
				test.Errorf("with nothing moved, the second ask was not answered with the first's listing")
			}
			if remembered, answered := fingerprints.ListingsRemembered(); remembered != 1 || answered != 1 {
				test.Fatalf("with nothing moved, %d listings were remembered and %d asks answered, want one each",
					remembered, answered)
			}
			writeListingFiles(test, directory, map[string]string{rewritten.file: rewritten.contents})
			third := ask()
			if third == first {
				test.Errorf("after %s was rewritten, the ask was answered with the listing made before", rewritten.file)
			}
			if fourth := ask(); fourth != third {
				test.Errorf("after %s was rewritten, the listing made against its new bytes was not remembered",
					rewritten.file)
			}
			if remembered, answered := fingerprints.ListingsRemembered(); remembered != 2 || answered != 2 {
				test.Errorf("%d listings were remembered and %d asks answered, want two each: one before %s was "+
					"rewritten and one after", remembered, answered, rewritten.file)
			}
		})
	}
}

// A listing with an error in it is never remembered (#32594zm), as none is kept: another ask could answer it otherwise.
// A module importing a package no module provides lists that package with its error, and two asks through one run's
// fingerprints each list it, neither answered from the other.
func TestAListingWithAnErrorIsNeverRemembered(test *testing.T) {
	test.Parallel()
	directory := test.TempDir()
	writeListingFiles(test, directory, map[string]string{
		"go.mod": "module example.com/erring\n\ngo 1.27\n",
		"a.go":   "package a\n\nimport _ \"example.com/missing\"\n",
	})
	goEnvironment := goEnvironmentOf(test, directory, []string{"GOWORK=off"})
	fingerprints := NewFingerprints()
	var listings []*listing
	for ask := range 2 {
		listed, _, listError := cachedList(goEnvironment, []string{"./..."}, nil, nil, NewSharedReads(), fingerprints,
			nil, nil)
		if listError != nil {
			test.Fatalf("ask %d: %v", ask, listError)
		}
		if missing := listed.byPath["example.com/missing"]; missing == nil || missing.Error == nil {
			test.Fatalf("ask %d: the missing import was listed as %+v, with no error to refuse", ask, missing)
		}
		listings = append(listings, listed)
	}
	if listings[1] == listings[0] {
		test.Error("the second ask was answered with the first's listing, which has an error in it")
	}
	if remembered, answered := fingerprints.ListingsRemembered(); remembered != 0 || answered != 0 {
		test.Errorf("%d listings were remembered and %d asks answered, want none", remembered, answered)
	}
}
