package load

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// A load handed go env's answer for its own workspace and environment reads it and asks go env nothing, and a load of
// another workspace or under another environment asks for itself. The answer carries a go env file planted only in it,
// which the inputs record names as absent, so the record says whose answer the load read (#f53yra8).
func TestALoadReadsTheGoEnvironmentItIsHanded(test *testing.T) {
	test.Parallel()
	root, resolveError := filepath.EvalSymlinks(test.TempDir())
	if resolveError != nil {
		test.Fatal(resolveError)
	}
	writeListingFiles(test, root, listingFiles)
	project := filepath.Join(root, "project")
	other := filepath.Join(root, "outside")
	environment := []string{"GOWORK=off"}
	planted := filepath.Join(root, "planted-go.env")

	answer, answerError := readGoEnvironment(project, environment, nil)
	if answerError != nil {
		test.Fatal(answerError)
	}
	answer.reported.environmentFile = planted
	readsPlanted := func(directory string, loadEnvironment []string) bool {
		test.Helper()
		loaded, loadError := PackagesCached(directory, []string{"./..."}, loadEnvironment, nil,
			Caching{GoEnvironment: answer})
		if loadError != nil {
			test.Fatal(loadError)
		}
		return slices.Contains(loaded.Inputs.Absent, planted)
	}
	if !readsPlanted(project, environment) {
		test.Error("a load handed go env's answer for its own workspace and environment asked go env again")
	}
	if readsPlanted(project, append(slices.Clone(environment), "CGO_ENABLED=0")) {
		test.Error("a load under another environment read an answer given for this one")
	}
	if readsPlanted(other, environment) {
		test.Error("a load of another workspace read an answer given for this one")
	}
	if _, statError := os.Stat(planted); statError == nil {
		test.Fatal("the planted go env file exists, so its absence proves nothing")
	}
}
