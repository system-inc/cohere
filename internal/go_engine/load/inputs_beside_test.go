package load

import (
	"errors"
	"maps"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// inputsHoldLimit is how long a test's hold on a load's inputs waits for the load to return before letting the
// collection go on: a load that collects beside returns at once, and one that collects before returning never would,
// so the limit only bounds how long such a load takes to fail the test rather than hang it.
const inputsHoldLimit = 20 * time.Second

// A load returns while its inputs are still being collected (#ybjbqm2): the collection is held until the load has
// returned, and once let go, the waiter gets the inputs whole, the same as a load whose collection nobody held.
func TestALoadReturnsBeforeItsInputsAreCollected(test *testing.T) {
	test.Parallel()

	loadedInputsFixture(test)
	returned := make(chan struct{})
	heldPastTheLoad := false
	caching := Caching{beforeInputs: func() {
		select {
		case <-returned:
			heldPastTheLoad = true
		case <-time.After(inputsHoldLimit):
		}
	}}
	loaded, loadError := PackagesCached(inputsFixture.module, []string{"./..."}, inputsFixtureEnvironment(),
		inputsFixtureCensus(), caching)
	close(returned)
	if loadError != nil {
		test.Fatal(loadError)
	}
	inputs := loaded.Inputs()
	// Read after the waiter, which returns only once the collection, and the hold before it, has ended.
	if !heldPastTheLoad {
		test.Errorf("the load returned only once its inputs were collected, after holding them %s", inputsHoldLimit)
	}
	unheld := inputsFixture.loaded.Inputs()
	if !slices.Equal(inputs.Files, unheld.Files) || !slices.Equal(inputs.Directories, unheld.Directories) ||
		!slices.Equal(inputs.Absent, unheld.Absent) {
		test.Errorf("the inputs collected beside the load\n%+v\ndiffer from an unheld load's\n%+v", inputs, unheld)
	}
}

// A panic while a load's inputs are collected reaches the goroutine that waits for them, with its own value, rather
// than ending the process from the goroutine the collection ran on.
func TestAPanicCollectingALoadsInputsReachesItsWaiter(test *testing.T) {
	test.Parallel()

	loadedInputsFixture(test)
	sentinel := errors.New("load: the sentinel panic collecting inputs")
	loaded, loadError := PackagesCached(inputsFixture.module, []string{"./..."}, inputsFixtureEnvironment(),
		inputsFixtureCensus(), Caching{beforeInputs: func() { panic(sentinel) }})
	if loadError != nil {
		test.Fatal(loadError)
	}
	recovered := func() (recovered any) {
		defer func() { recovered = recover() }()
		loaded.Inputs()
		return nil
	}()
	// Compared by identity, so a wrapped or copied value fails.
	if recovered != sentinel {
		test.Errorf("the waiter raised %v, want the sentinel itself", recovered)
	}
}

// Two loads sharing the run's walks of their module roots walk each root once between them: the module, its sibling
// replacement and the directory above it, which the other replacement names. Each names the directories a load
// sharing nothing names.
func TestLoadsSharingModuleRootWalksWalkEachRootOnce(test *testing.T) {
	test.Parallel()

	loadedInputsFixture(test)
	walks := NewModuleRootWalks()
	unshared := inputsFixture.loaded.Inputs().Directories
	for round := range 2 {
		loaded, loadError := PackagesCached(inputsFixture.module, []string{"./..."}, inputsFixtureEnvironment(),
			inputsFixtureCensus(), Caching{ModuleRootWalks: walks})
		if loadError != nil {
			test.Fatal(loadError)
		}
		if directories := loaded.Inputs().Directories; !slices.Equal(directories, unshared) {
			test.Errorf("load %d named the directories\n%q\nand a load sharing no walks\n%q", round+1, directories,
				unshared)
		}
		if walked := walks.walked.Load(); walked != 3 {
			test.Errorf("after load %d the roots were walked %d times, want 3, each once", round+1, walked)
		}
	}
	// Read under the lock each load took to add its roots.
	walks.mutex.Lock()
	roots := slices.Sorted(maps.Keys(walks.walks))
	walks.mutex.Unlock()
	want := []string{inputsFixture.root, filepath.Join(inputsFixture.root, "module"),
		filepath.Join(inputsFixture.root, "sibling")}
	if !slices.Equal(roots, want) {
		test.Errorf("the walked roots are %q, want %q", roots, want)
	}
}
