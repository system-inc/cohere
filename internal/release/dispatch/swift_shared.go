package dispatch

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SwiftEngineSharedVariable names a read-only folder of cohere-swift binaries built elsewhere, each under the name
// SwiftBinaryPath gives it beside `<name>.sha256`, its sha256 in lowercase hex on one line. A binary missing from
// the cache is taken from there instead of built, once its bytes match its sum (takeSharedSwiftEngine).
//
// It is for a job whose cache is emptied every run, Loom's integrate job on Shackler, which built cohere-swift from
// nothing each time, about 3.5 minutes (#e94a6r4). Loom fills the folder from SwiftEngineLayerVariable's layers.
// Unset, nothing is looked for.
//
// The name is Loom's, not cohere's: its runner binds every cache a job kind declares as `LOOM_CACHE_<NAME>_SHARED`
// and `LOOM_CACHE_<NAME>_LAYER`, and this cache is named `swift`. Spelled here once, so a rename is one line.
const SwiftEngineSharedVariable = "LOOM_CACHE_SWIFT_SHARED"

// SwiftEngineLayerVariable names the job's own writable folder, where every cohere-swift built here is left in the
// shape SwiftEngineSharedVariable reads (fillSwiftEngineLayer). Loom promotes a fill job's layer into the shared
// folder. Unset, a build is left in the cache alone. Loom's name, as SwiftEngineSharedVariable's is.
const SwiftEngineLayerVariable = "LOOM_CACHE_SWIFT_LAYER"

// swiftEngineSumSuffix ends the name of the file holding a shared binary's sum.
const swiftEngineSumSuffix = ".sha256"

// swiftEngineFolder is the folder variable names, or empty when it names none. A relative path is refused: it would
// be read against whatever directory cohere runs in, a different folder for every project.
func swiftEngineFolder(variable string) (string, error) {
	folder := strings.TrimSpace(os.Getenv(variable))
	if folder != "" && !filepath.IsAbs(folder) {
		return "", fmt.Errorf("%s=%q is no folder's whole path, so cohere-swift was not resolved", variable, folder)
	}
	return folder, nil
}

// takeSharedSwiftEngine copies the shared folder's cohere-swift named as binaryPath is to destination, and reports
// whether it did.
//
// The binary is used only when the sha256 of the copy at destination is the one its sum file holds. The copy is what
// gets run, so it is what is hashed: a shared file that changed between a check and the copy would otherwise run
// bytes nothing vouched for. A sum that differs, or one missing or not a sha256, is a miss, said on stderr in one
// line, any copy is removed, and the caller builds as it would have. A binary the folder doesn't hold is a miss said
// nowhere, the ordinary case. Copied with copyExecutable, as a build's product is placed, so what follows is the
// same either way.
func takeSharedSwiftEngine(binaryPath string, destination string) (bool, error) {
	shared, swiftEngineFolderError := swiftEngineFolder(SwiftEngineSharedVariable)
	if swiftEngineFolderError != nil || shared == "" {
		return false, swiftEngineFolderError
	}
	entry := filepath.Join(shared, filepath.Base(binaryPath))
	if info, statError := os.Stat(entry); statError != nil || !info.Mode().IsRegular() {
		return false, nil
	}

	sumPath := entry + swiftEngineSumSuffix
	contents, readError := os.ReadFile(sumPath)
	if readError != nil {
		Report.Fail("cohere: the shared cohere-swift %s was not used, its sum %s could not be read (%v); building it",
			entry, sumPath, readError)
		return false, nil
	}
	recorded := strings.TrimSpace(string(contents))

	if createDirectoryError := os.MkdirAll(filepath.Dir(destination), 0o755); createDirectoryError != nil {
		return false, fmt.Errorf("creating the binary cache directory: %w", createDirectoryError)
	}
	if copyExecutableError := copyExecutable(entry, destination); copyExecutableError != nil {
		return false, fmt.Errorf("taking the shared cohere-swift %s into the cache: %w", entry, copyExecutableError)
	}
	actual, fileSumError := fileSum(destination)
	if fileSumError != nil || actual != recorded {
		// Not ignored: the build that follows installs through copyExecutable, which keeps a file already at its
		// destination, so a copy left here would be run in place of the build.
		if removeError := os.Remove(destination); removeError != nil {
			return false, fmt.Errorf("removing %s, a copy of the shared cohere-swift %s its sum does not vouch for: %w",
				destination, entry, removeError)
		}
		if fileSumError != nil {
			Report.Fail("cohere: the shared cohere-swift %s was not used, its copy at %s could not be read (%v); "+
				"building it", entry, destination, fileSumError)
			return false, nil
		}
		Report.Fail("cohere: the shared cohere-swift %s was not used, its copy's sha256 is %s and %s holds %q; "+
			"building it", entry, actual, sumPath, recorded)
		return false, nil
	}
	return true, nil
}

// fillSwiftEngineLayer leaves the cohere-swift just built at binaryPath in the job's layer, under its own name
// beside its sum, when SwiftEngineLayerVariable names one.
//
// The binary goes in through copyExecutable and the sum through a temporary name and a rename, so a reader never
// sees half of either, and the sum is written last and read from the copy, so it vouches for the bytes beside it.
// A layer that can't be written fails the run: a fill that quietly left nothing would leave every later job to build
// from nothing, with nobody told why.
func fillSwiftEngineLayer(binaryPath string) error {
	layer, swiftEngineFolderError := swiftEngineFolder(SwiftEngineLayerVariable)
	if swiftEngineFolderError != nil || layer == "" {
		return swiftEngineFolderError
	}
	entry := filepath.Join(layer, filepath.Base(binaryPath))
	if createDirectoryError := os.MkdirAll(layer, 0o755); createDirectoryError != nil {
		return fmt.Errorf("creating the cohere-swift layer %s: %w", layer, createDirectoryError)
	}
	if copyExecutableError := copyExecutable(binaryPath, entry); copyExecutableError != nil {
		return fmt.Errorf("leaving cohere-swift in the layer as %s: %w", entry, copyExecutableError)
	}
	sum, fileSumError := fileSum(entry)
	if fileSumError != nil {
		return fmt.Errorf("reading the sha256 of %s: %w", entry, fileSumError)
	}

	temporary, temporaryFileError := os.CreateTemp(layer, filepath.Base(entry)+swiftEngineSumSuffix+".partial-*")
	if temporaryFileError != nil {
		return fmt.Errorf("writing the sum of %s: %w", entry, temporaryFileError)
	}
	// Ignored: after the rename the name is gone and this fails by design, and on a failure path the error returned
	// is the one that says what went wrong.
	defer os.Remove(temporary.Name())
	_, writeError := temporary.WriteString(sum + "\n")
	// Readable by all, as the binary beside it is: CreateTemp makes a file its owner alone can read, and whoever
	// promotes the layer and every job reading the shared folder may not be its owner.
	chmodError := temporary.Chmod(0o644)
	closeError := temporary.Close()
	if writeError = errors.Join(writeError, chmodError, closeError); writeError != nil {
		return fmt.Errorf("writing the sum of %s: %w", entry, writeError)
	}
	if renameError := os.Rename(temporary.Name(), entry+swiftEngineSumSuffix); renameError != nil {
		return fmt.Errorf("writing the sum of %s: %w", entry, renameError)
	}
	return nil
}
