package bounded

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
)

// panicCaseVariable names, in a child test process's environment, the one case of
// TestAPanicInAWorkerReachesTheCaller the child runs.
const panicCaseVariable = "COHERE_TEST_BOUNDED_PANIC_CASE"

// A panic in one index's work of a fan-out reaches the caller's goroutine, raised again with its own value once every
// index has run, rather than ending the process from a worker (#z8ym3x5): the caller recovers the sentinel itself,
// every other index ran, and every slot is free after, the pool's held count back to zero, and for ForEach, whose
// slots are its own, a second call over a single slot runs every index. ForEachWriting carries a panic in compute and
// one in a write alike.
//
// Each case runs in a child test process, since a panic a fan-out fails to carry ends the process it happens in: the
// child's death, or its failed check, is this test's failure, with the child's output. The child runs on one core, so
// ForEach has a single slot and a slot the panicking index kept would hold every later index waiting until the child's
// timeout.
func TestAPanicInAWorkerReachesTheCaller(test *testing.T) {
	test.Parallel()
	const count = 64
	const panicking = 17
	cases := []struct {
		name string
		run  func(test *testing.T, sentinel error) (ran []atomic.Bool, recovered any)
	}{
		{"ForEach", func(test *testing.T, sentinel error) ([]atomic.Bool, any) {
			ran := make([]atomic.Bool, count)
			recovered := recovering(func() {
				ForEach(count, false, func(index int) {
					if index == panicking {
						panic(sentinel)
					}
					ran[index].Store(true)
				})
			})
			var again atomic.Int64
			ForEach(count, false, func(int) { again.Add(1) })
			if again.Load() != count {
				test.Errorf("a second call after the panic ran %d indexes, want %d", again.Load(), count)
			}
			return ran, recovered
		}},
		{"PoolForEach", func(test *testing.T, sentinel error) ([]atomic.Bool, any) {
			ran := make([]atomic.Bool, count)
			pool := NewPool(4)
			recovered := recovering(func() {
				pool.ForEach(count, 0, func(index int) {
					if index == panicking {
						panic(sentinel)
					}
					ran[index].Store(true)
				})
			})
			expectEverySlotFree(test, pool)
			return ran, recovered
		}},
		{"ForEachWritingCompute", func(test *testing.T, sentinel error) ([]atomic.Bool, any) {
			ran := make([]atomic.Bool, count)
			pool := NewPool(4)
			recovered := recovering(func() {
				pool.ForEachWriting(count, 0, func(index int) func() {
					if index == panicking {
						panic(sentinel)
					}
					return func() { ran[index].Store(true) }
				})
			})
			expectEverySlotFree(test, pool)
			return ran, recovered
		}},
		{"ForEachWritingWrite", func(test *testing.T, sentinel error) ([]atomic.Bool, any) {
			ran := make([]atomic.Bool, count)
			pool := NewPool(4)
			recovered := recovering(func() {
				pool.ForEachWriting(count, 0, func(index int) func() {
					return func() {
						if index == panicking {
							panic(sentinel)
						}
						ran[index].Store(true)
					}
				})
			})
			expectEverySlotFree(test, pool)
			return ran, recovered
		}},
	}
	for _, panicCase := range cases {
		test.Run(panicCase.name, func(test *testing.T) {
			test.Parallel()
			if os.Getenv(panicCaseVariable) == panicCase.name {
				sentinel := errors.New("bounded: the sentinel panic")
				ran, recovered := panicCase.run(test, sentinel)
				// Compared by identity, so a wrapped or copied value fails.
				if recovered != sentinel {
					test.Errorf("the caller recovered %v, want the sentinel itself", recovered)
				}
				for index := range ran {
					if index != panicking && !ran[index].Load() {
						test.Errorf("index %d never ran after index %d panicked", index, panicking)
					}
				}
				if ran[panicking].Load() {
					test.Errorf("the panicking index %d ran on past its panic", panicking)
				}
				return
			}
			executable, executableError := os.Executable()
			if executableError != nil {
				test.Fatal(executableError)
			}
			command := exec.Command(
				executable,
				"-test.run=^TestAPanicInAWorkerReachesTheCaller$/^"+panicCase.name+"$",
				"-test.v",
				"-test.count=1",
				"-test.timeout=60s",
			)
			// One core, so ForEach's single slot shows a slot kept by the panicking index as a wait until the timeout.
			command.Env = append(os.Environ(), panicCaseVariable+"="+panicCase.name, "GOMAXPROCS=1")
			output, outputError := command.CombinedOutput()
			// The pass line proves the child ran this case and not an empty match.
			passed := "--- PASS: TestAPanicInAWorkerReachesTheCaller/" + panicCase.name
			if outputError != nil || !strings.Contains(string(output), passed) {
				test.Fatalf("the child running %s failed (%v):\n%s", panicCase.name, outputError, output)
			}
		})
	}
}

// recovering runs call and returns what it panicked with, nil when it returned.
func recovering(call func()) (recovered any) {
	defer func() { recovered = recover() }()
	call()
	return nil
}

// expectEverySlotFree fails test unless every one of pool's slots is free and none is held or waited for.
func expectEverySlotFree(test *testing.T, pool *Pool) {
	test.Helper()
	pool.lock.Lock()
	defer pool.lock.Unlock()
	if pool.held != 0 || pool.free != pool.size || len(pool.waiting) != 0 {
		test.Errorf("after the panic, %d slots are held, %d of %d free and %d wait, want 0, all and 0", pool.held,
			pool.free, pool.size, len(pool.waiting))
	}
}
