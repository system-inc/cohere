package program_test

import (
	"context"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/internal/types/program"
)

// spinSink keeps the compiler from deleting the spin as dead code.
var spinSink atomic.Uint64

// spin burns a fixed amount of CPU. Fixed work rather than spinning on the clock, because a goroutine
// descheduled while it watched the clock would spend wall time without CPU, which is the very
// confusion under test.
func spin(iterations int) {
	value := uint64(1)
	for range iterations {
		value = value*6364136223846793005 + 1442695040888963407
	}
	spinSink.Add(value)
}

// processCPU is the CPU this process has used, user and system, as the kernel counts it.
func processCPU(t *testing.T) time.Duration {
	t.Helper()
	var usage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
		t.Fatalf("reading process CPU: %v", err)
	}
	return time.Duration(usage.Utime.Nano() + usage.Stime.Nano())
}

// TestATimedWalkBillsCPUNotWaiting is the control the instrument is trusted on (#8qyzmxw).
//
// The table it replaced charged each listener call its wall time, so a rule descheduled mid-call was
// billed for the time it spent off the CPU, and the better-tailwindcss family read about 1.2s when its
// real cost was about 0.05s. Two planted rules tell the two instruments apart. One sleeps in every file:
// it waits and spends no CPU, so it must read near zero, where a wall clock bills it every millisecond
// it slept. The other spins a fixed amount of work in every file: it must read the CPU that work costs,
// calibrated here by the kernel's own count rather than by the instrument under test.
//
// The sleeper's wall time is measured too, so the test proves it could have failed: a wall clock around
// the same calls would have read that number.
func TestATimedWalkBillsCPUNotWaiting(t *testing.T) {
	const fileCount = 80
	const sleepPerFile = 5 * time.Millisecond

	// Calibrate one spin call to about 5ms of CPU, by the kernel's count.
	iterations := 1_000_000
	for {
		before := processCPU(t)
		for range 20 {
			spin(iterations)
		}
		perCall := (processCPU(t) - before) / 20
		if perCall >= 4*time.Millisecond {
			break
		}
		iterations *= 2
	}
	before := processCPU(t)
	for range fileCount {
		spin(iterations)
	}
	expectedSpin := processCPU(t) - before

	files := map[string]string{"tsconfig.json": minimalConfig}
	for index := range fileCount {
		files[fmt.Sprintf("file%02d.ts", index)] = fmt.Sprintf("export const value%d = %d;\n", index, index)
	}
	root := writeProject(t, files)
	graph, err := program.Build(program.Options{ConfigFileName: filepath.Join(root, "tsconfig.json")})
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	var sleptTotal atomic.Int64
	sleeper := rule.Rule{
		Name: "planted-sleeper",
		Run: func(ctx rule.Context, options any) rule.Listeners {
			return rule.Listeners{ast.KindSourceFile: func(node *ast.Node) {
				start := time.Now()
				time.Sleep(sleepPerFile)
				sleptTotal.Add(int64(time.Since(start)))
			}}
		},
	}
	spinner := rule.Rule{
		Name: "planted-spinner",
		Run: func(ctx rule.Context, options any) rule.Listeners {
			return rule.Listeners{ast.KindSourceFile: func(node *ast.Node) {
				spin(iterations)
			}}
		},
	}

	graph.CollectTimings = true
	result, err := graph.Walk(context.Background(), graph.ProjectFiles(), []rule.Rule{sleeper, spinner})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if result.Timings.Account.Unavailable != "" {
		t.Fatalf("the walk was not measured, so this proves nothing: %s", result.Timings.Account.Unavailable)
	}

	byName := map[string]program.RuleTiming{}
	for _, timing := range result.Timings.Sorted() {
		byName[timing.Name] = timing
	}
	slept, spun := byName["planted-sleeper"], byName["planted-spinner"]

	// Both rules ran in every file, or a zero below would be a rule that never ran.
	if slept.NodesOffered != fileCount || spun.NodesOffered != fileCount {
		t.Fatalf("the planted rules ran on %d and %d files, want %d each", slept.NodesOffered, spun.NodesOffered, fileCount)
	}
	sleptWall := time.Duration(sleptTotal.Load())
	if sleptWall < fileCount*sleepPerFile {
		t.Fatalf("the sleeper slept %v, under the %v it was planted with, so it cannot show a wall clock's error",
			sleptWall, fileCount*sleepPerFile)
	}

	// The sleeper's CPU is the few instructions around each sleep: about 28µs per 50ms slept, measured on
	// darwin/arm64, so under 2ms here against the 400ms a wall clock bills it.
	if slept.TotalCPU() > 2*time.Millisecond {
		t.Fatalf("a rule that only sleeps was billed %v of CPU, having slept %v", slept.TotalCPU(), sleptWall)
	}

	// The spinner is measured by its thread's clock and the reference by the process's, which also counts
	// the garbage collector and the runtime during calibration, so the two agree to within a fifth.
	if got := spun.TotalCPU(); got < expectedSpin*4/5 || got > expectedSpin*6/5 {
		t.Fatalf("a rule that spins %v of CPU (by the kernel's count) was billed %v", expectedSpin, got)
	}
	t.Logf("spinner billed %v against %v by the kernel; sleeper billed %v after %v asleep",
		spun.TotalCPU(), expectedSpin, slept.TotalCPU(), sleptWall)
}
