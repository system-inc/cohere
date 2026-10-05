package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestWholeModuleAndCountOnceAreReadFromTheArguments(t *testing.T) {
	t.Parallel()
	if !wholeModule([]string{"-race", "./..."}) || wholeModule([]string{"./internal/edit"}) {
		t.Error("whole-module detection is wrong")
	}
	for _, arguments := range [][]string{{"-count=1", "./..."}, {"-count", "1", "./..."}, {"--count=1", "./..."}} {
		if !countsOnce(arguments) {
			t.Errorf("%v does not read as -count=1", arguments)
		}
	}
	if countsOnce([]string{"-count=2", "./..."}) {
		t.Error("-count=2 reads as -count=1")
	}
}

// A slot held is a slot no one else takes until it is released, and a second slot is free while the first
// is held.
func TestASlotIsHeldUntilReleased(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("slots are a Unix lock")
	}
	directory := t.TempDir()
	first, err := takeSlot(directory, 1, []string{"./..."})
	if err != nil || first == nil {
		t.Fatalf("an empty machine gave no slot: %v", err)
	}
	if second, err := takeSlot(directory, 1, []string{"./..."}); err != nil || second != nil {
		t.Fatalf("a held slot was taken again (%v)", err)
	}
	if lines := holders(directory, 1); len(lines) != 1 || !strings.Contains(lines[0], "go test ./...") {
		t.Fatalf("the holder is not named: %v", lines)
	}
	if two, err := takeSlot(directory, 2, []string{"./..."}); err != nil || two == nil {
		t.Fatalf("with two slots the second was not free (%v)", err)
	} else {
		two.release()
	}
	first.release()
	if again, err := takeSlot(directory, 1, []string{"./..."}); err != nil || again == nil {
		t.Fatalf("a released slot was not free (%v)", err)
	} else {
		again.release()
	}
}

// Two whole-module runs on a one-slot machine: the second says it is waiting and whose run holds the slot,
// and starts when the first ends. A -count=1 whole-module run is refused before anything runs.
func TestASecondWholeModuleRunWaitsAndSaysForWhom(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("slots are a Unix lock")
	}
	wrapper := filepath.Join(t.TempDir(), "cohere-dev")
	if output, err := exec.Command("go", "build", "-o", wrapper, ".").CombinedOutput(); err != nil {
		t.Fatalf("building: %v\n%s", err, output)
	}
	// A go that takes two seconds to test anything, on the front of the path. It leaves a mark as it starts,
	// which only a run holding the slot reaches, so the second run starts once the first holds it. A fixed
	// stagger instead lost the race under the house's load, where the second run's process could start
	// first and take the slot.
	bin := t.TempDir()
	started := filepath.Join(bin, "started")
	script := "#!/bin/sh\ntouch '" + started + "'\nsleep 2\necho tested \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "go"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	environment := append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"HOME="+home, "XDG_CACHE_HOME="+filepath.Join(home, "cache"), slotsVariable+"=1")

	run := func(arguments ...string) (string, error) {
		command := exec.Command(wrapper, append([]string{"test"}, arguments...)...)
		command.Env = environment
		output, err := command.CombinedOutput()
		return string(output), err
	}

	began := time.Now()
	outputs := make([]string, 2)
	var group sync.WaitGroup
	for index := range outputs {
		group.Add(1)
		go func() {
			defer group.Done()
			if index == 1 {
				for _, err := os.Stat(started); err != nil; _, err = os.Stat(started) {
					if time.Since(began) > time.Minute {
						t.Errorf("the first run never started testing")
						return
					}
					time.Sleep(10 * time.Millisecond)
				}
			}
			output, err := run("./...")
			if err != nil {
				t.Errorf("run %d failed: %v\n%s", index, err, output)
			}
			outputs[index] = output
		}()
	}
	group.Wait()
	if elapsed := time.Since(began); elapsed < 4*time.Second {
		t.Errorf("two runs on one slot finished in %s, so they overlapped", elapsed)
	}
	if !strings.Contains(outputs[1], "waiting for a test slot") || !strings.Contains(outputs[1], "go test ./...") ||
		!strings.Contains(outputs[1], "took a test slot after waiting") {
		t.Errorf("the second run did not say it waited and for whom:\n%s", outputs[1])
	}
	if strings.Contains(outputs[0], "waiting") {
		t.Errorf("the first run waited:\n%s", outputs[0])
	}

	output, err := run("-count=1", "./...")
	if err == nil || !strings.Contains(output, "never passes -count=1") || strings.Contains(output, "tested") {
		t.Errorf("a -count=1 whole-module run was not refused before running:\n%s", output)
	}
	if output, err := run("-count=1", "./internal/edit"); err != nil || !strings.Contains(output, "tested test -count=1 ./internal/edit") {
		t.Errorf("a -count=1 run of one package was held back:\n%s (%v)", output, err)
	}
}
