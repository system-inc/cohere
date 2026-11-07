package main

import (
	"bufio"
	"fmt"

	"golang.org/x/tools/go/packages"
)

// expectation is a whole load's fingerprints and imports as they must read after the moves, by package ID after
// the moves.
type expectation struct {
	identifiers map[string]map[string][]record
	imports     map[string]map[string][]string
	// files limits the comparison to these translated files when set: a platform's load is compared only on the
	// files no earlier load read.
	files map[string]bool
}

// compare checks a load taken after the moves against the expectation, writing each mismatch to output, and returns
// how many identifiers it compared and how many mismatched. A package of the load whose path the moves would
// still move is the old folder's, which an overlay leaves on disk, and is not compared.
func (expected *expectation) compare(roots []*packages.Package, mapping *mapping, output *bufio.Writer) (int, int) {
	compared, mismatches := 0, 0
	seen := map[string]bool{}
	for _, rootsPackage := range roots {
		if mapping.NewPackageId(rootsPackage.ID) != rootsPackage.ID {
			continue
		}
		seen[rootsPackage.ID] = true
		wantIdentifiers, found := expected.identifiers[rootsPackage.ID]
		if !found {
			if expected.files == nil {
				fmt.Fprintf(output, "mismatch: package %s is new after the moves\n", rootsPackage.ID)
				mismatches++
			}
			continue
		}
		gotIdentifiers := fingerprint(rootsPackage, translation{file: identity, packagePath: identity})
		gotImports := importsOf(rootsPackage, translation{file: identity, packagePath: identity}, nil)
		for _, filename := range sortedKeys(wantIdentifiers) {
			if expected.files != nil && !expected.files[filename] {
				continue
			}
			want, got := wantIdentifiers[filename], gotIdentifiers[filename]
			if got == nil {
				fmt.Fprintf(output, "mismatch: %s in %s is missing after the moves\n", filename, rootsPackage.ID)
				mismatches++
				continue
			}
			if len(want) != len(got) {
				fmt.Fprintf(
					output,
					"mismatch: %s in %s: %d identifiers expected, %d found\n",
					filename,
					rootsPackage.ID,
					len(want),
					len(got),
				)
				mismatches++
				continue
			}
			for index := range want {
				compared++
				if want[index] != got[index] {
					fmt.Fprintf(
						output,
						"mismatch: %s in %s, identifier %d: expected %+v, found %+v\n",
						filename,
						rootsPackage.ID,
						index,
						want[index],
						got[index],
					)
					mismatches++
				}
			}
			if fmt.Sprint(expected.imports[rootsPackage.ID][filename]) != fmt.Sprint(gotImports[filename]) {
				fmt.Fprintf(
					output,
					"mismatch: %s in %s imports %v, expected %v\n",
					filename,
					rootsPackage.ID,
					gotImports[filename],
					expected.imports[rootsPackage.ID][filename],
				)
				mismatches++
			}
		}
		if expected.files == nil && len(gotIdentifiers) != len(wantIdentifiers) {
			fmt.Fprintf(
				output,
				"mismatch: %s has %d files after the moves, %d expected\n",
				rootsPackage.ID,
				len(gotIdentifiers),
				len(wantIdentifiers),
			)
			mismatches++
		}
	}
	if expected.files == nil {
		for _, id := range sortedKeys(expected.identifiers) {
			if !seen[id] {
				fmt.Fprintf(output, "mismatch: package %s is gone after the moves\n", id)
				mismatches++
			}
		}
	}
	return compared, mismatches
}
