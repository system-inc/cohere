package main

import (
	"sync"
	"testing"
)

// scheduleFixture is four of ours: a imports nothing, b and c import a, d imports b and c.
func scheduleFixture() ([]*ourPackage, map[string]*ourPackage) {
	a := &ourPackage{path: "a"}
	b := &ourPackage{path: "b", imports: []string{"a", "fmt"}}
	c := &ourPackage{path: "c", imports: []string{"a"}}
	d := &ourPackage{path: "d", imports: []string{"b", "c"}}
	ordered := []*ourPackage{a, b, c, d}
	return ordered, map[string]*ourPackage{"a": a, "b": b, "c": c, "d": d}
}

// TestRunScheduleStartsAPackageAfterItsImports runs the fixture on several workers and requires each package to start
// only after every one of ours it imports has finished, and each to run once.
func TestRunScheduleStartsAPackageAfterItsImports(t *testing.T) {
	t.Parallel()
	ordered, ours := scheduleFixture()
	var lock sync.Mutex
	finished := map[string]bool{}
	runs := map[string]int{}
	run := runSchedule(ordered, ours, 3, func(our *ourPackage) int {
		lock.Lock()
		defer lock.Unlock()
		for _, imported := range our.imports {
			if _, isOurs := ours[imported]; isOurs && !finished[imported] {
				t.Errorf("%s started before %s finished", our.path, imported)
			}
		}
		runs[our.path]++
		finished[our.path] = true
		return 1
	})
	for _, our := range ordered {
		if runs[our.path] != 1 {
			t.Errorf("%s ran %d times", our.path, runs[our.path])
		}
	}
	if run.Missed != len(ordered) || len(run.JobSeconds) != len(ordered) {
		t.Errorf("missed %d and timed %d jobs, want %d of each", run.Missed, len(run.JobSeconds), len(ordered))
	}
}

// TestCriticalPathIsTheLongestPathThroughOurs weighs the fixture so the path through c is the longest.
func TestCriticalPathIsTheLongestPathThroughOurs(t *testing.T) {
	t.Parallel()
	ordered, ours := scheduleFixture()
	path := criticalPath(ordered, ours, map[string]float64{"a": 1, "b": 2, "c": 5, "d": 1})
	if path != 7 {
		t.Errorf("critical path %v, want 7 (a, c, d)", path)
	}
}
