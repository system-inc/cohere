package load_memo

import (
	"go/token"
	"runtime"
	"testing"

	"github.com/system-inc/go-types/types"
)

// countingMemo is a memo whose compute counts each package it is asked to read, and reads off the package its path.
func countingMemo() (*Memo[string], map[*types.Package]int) {
	computed := map[*types.Package]int{}
	return New(func(declaring *types.Package) string {
		computed[declaring]++
		return declaring.Path()
	}), computed
}

// A load reads each package once however often its packages ask, and every ask is handed what that read gave. A
// second load, parsed into a file set of its own, reads the same package again, since its entry would hold another
// load's types.
func TestALoadReadsEachPackageOnce(test *testing.T) {
	test.Parallel()
	memo, computed := countingMemo()
	first := types.NewPackage("example.com/first", "first")
	second := types.NewPackage("example.com/second", "second")
	fileSet := token.NewFileSet()
	for range 3 {
		if value := memo.Load(fileSet).Of(first); value != "example.com/first" {
			test.Fatalf("want the first package's path, got %q", value)
		}
	}
	memo.Load(fileSet).Of(second)
	if computed[first] != 1 || computed[second] != 1 {
		test.Errorf("one load read the first package %d times and the second %d, where each must be read once",
			computed[first], computed[second])
	}
	otherFileSet := token.NewFileSet()
	memo.Load(otherFileSet).Of(first)
	if computed[first] != 2 {
		test.Errorf("a second load read the first package %d times in all, where it must read it again, twice",
			computed[first])
	}
	runtime.KeepAlive(fileSet)
	runtime.KeepAlive(otherFileSet)
}

// An entry does not keep its load alive, and goes once the load's file set is collected, so a run that loads the
// tree again for a rename's round does not hold every earlier load's types.
func TestALoadIsLetGoWithItsFileSet(test *testing.T) {
	test.Parallel()
	memo, _ := countingMemo()
	declaring := types.NewPackage("example.com/declaring", "declaring")
	func() {
		memo.Load(token.NewFileSet()).Of(declaring)
	}()
	held := func() int {
		memo.mutex.Lock()
		defer memo.mutex.Unlock()
		return len(memo.byLoad)
	}
	// The cleanup that lets the entry go runs on the runtime's own goroutine after a collection finds the file set
	// unreachable, so the test collects and yields until it has run, a bounded number of times.
	for attempt := 0; attempt < 100 && held() > 0; attempt++ {
		runtime.GC()
		runtime.Gosched()
	}
	if loads := held(); loads != 0 {
		test.Errorf("the memo kept the load after its file set was collected, holding %d in all", loads)
	}
}
