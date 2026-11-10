//go:build rename_differential

package global_rename

import (
	"fmt"
	"log"
	"maps"
	"os"
	"slices"

	"github.com/system-inc/cohere/internal/go_rename/workspace"
)

// A build with the rename_differential tag proves every platform expectation it builds from a stored difference
// against the full way, a fresh load of the platform's before side, fingerprinted and renamed: a measurement of the
// difference on a real fixing run, which no untagged build carries (checkDifference).
func init() {
	checkDifference = differential
}

// differentialLog says what each check compared, on standard error, where a fixing run's log catches it.
var differentialLog = log.New(os.Stderr, "rename differential: ", 0)

// differential loads platform's before side fresh, renames its fingerprint by fromDifference's renames, and panics
// on any package, file, record or offset where fromDifference, built from difference, reads otherwise, naming the
// platform, package ID and file; a fresh load that fails is said and left unchecked. It logs one line per proof: the
// platform, the marked files and kept files checked, and the records compared.
func differential(
	loadPlatform func(platform workspace.Platform, overlay map[string][]byte) (*workspace.PlatformLoad, error),
	platform workspace.Platform,
	difference platformDifference,
	fromDifference *Expectation,
) {
	load, loadError := loadPlatform(platform, nil)
	if loadError != nil {
		// Nothing to compare: the proof's own load fails the same way and reads as a PlatformLoad mismatch.
		differentialLog.Printf("%s: not checked, the fresh before load failed: %v", platform, loadError)
		return
	}
	full := expectedFingerprint(load.Clean, fromDifference.newNameAt)
	ids := slices.Sorted(maps.Keys(full.Fingerprints))
	if !slices.Equal(ids, slices.Sorted(maps.Keys(fromDifference.Fingerprints))) {
		differs("%s: the full way has packages %v, the difference %v", platform, ids,
			slices.Sorted(maps.Keys(fromDifference.Fingerprints)))
	}
	markedFiles, keptFiles, records := 0, 0, 0
	for _, id := range ids {
		filenames := slices.Sorted(maps.Keys(full.Fingerprints[id]))
		if !slices.Equal(filenames, slices.Sorted(maps.Keys(fromDifference.Fingerprints[id]))) {
			differs("%s, package %s: the full way has files %v, the difference %v", platform, id, filenames,
				slices.Sorted(maps.Keys(fromDifference.Fingerprints[id])))
		}
		for _, filename := range filenames {
			expected, actual := full.Fingerprints[id][filename], fromDifference.Fingerprints[id][filename]
			if !slices.Equal(expected, actual) {
				differs("%s, package %s, file %s: the full way's %d records differ from the difference's %d "+
					"(marked %t)", platform, id, filename, len(expected), len(actual),
					difference.sameAsHost[id][filename])
			}
			if !slices.Equal(full.Offsets[id][filename], fromDifference.Offsets[id][filename]) {
				differs("%s, package %s, file %s: the offsets differ (marked %t)", platform, id, filename,
					difference.sameAsHost[id][filename])
			}
			if difference.sameAsHost[id][filename] {
				markedFiles++
			} else {
				keptFiles++
			}
			records += len(expected)
		}
	}
	differentialLog.Printf("%s: %d marked files and %d kept files checked, %d records compared", platform,
		markedFiles, keptFiles, records)
}

// differs stops the run on a place the difference reads otherwise than the full way, said by format and arguments.
func differs(format string, arguments ...any) {
	// Invariant: a marked file's records and offsets are the host's, and the same records at the same offsets take
	// the same renames, so the difference reads as the full way; a panic here is the differential's finding, and a
	// rename_differential build exists only to make it.
	panic("rename differential: " + fmt.Sprintf(format, arguments...))
}
