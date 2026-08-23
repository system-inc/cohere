package main

import (
	"os/exec"
	"sort"
	"strings"
	"testing"

	"github.com/system-inc/verify/internal/registry"
)

// The flag exists so a caller can tell a binary that lacks a rule from one whose rule found
// nothing. Those produce identical finding lists, and only this can separate them.
func TestRulesFlagListsEveryRegisteredRule(t *testing.T) {
	binary := t.TempDir() + "/verify"
	build := exec.Command("go", "build", "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Skipf("cannot build: %v\n%s", err, output)
	}

	output, err := exec.Command(binary, "-rules").Output()
	if err != nil {
		t.Fatalf("running -rules: %v", err)
	}
	printed := strings.Fields(string(output))

	expected := make([]string, 0, len(registry.All()))
	for _, registered := range registry.All() {
		expected = append(expected, registered.Name)
	}
	sort.Strings(expected)

	if len(printed) != len(expected) {
		t.Fatalf("printed %d rules, registry has %d", len(printed), len(expected))
	}
	for index := range expected {
		if printed[index] != expected[index] {
			t.Fatalf("at %d: printed %q, registry has %q", index, printed[index], expected[index])
		}
	}
	if len(printed) == 0 {
		t.Fatal("printed nothing, which would pass every comparison vacuously")
	}
}
