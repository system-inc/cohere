package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
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

// Waiters take the slot in the order they arrived, and one that dies while waiting is passed over rather
// than waited on (#nf1qj58). Five runs on a one-slot machine: the first holds the slot, three more queue
// behind it in a known order, the middle one is killed while it waits, and the rest run in arrival order.
// With the old polling, whichever waiter polled first after a release went next, so this order held by
// chance one time in six.
func TestWaitersTakeTheSlotInArrivalOrder(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("slots are a Unix lock")
	}
	wrapper := filepath.Join(t.TempDir(), "cohere-dev")
	if output, err := exec.Command("go", "build", "-o", wrapper, ".").CombinedOutput(); err != nil {
		t.Fatalf("building: %v\n%s", err, output)
	}
	bin := t.TempDir()
	order := filepath.Join(bin, "order")
	home := t.TempDir()
	slots := filepath.Join(home, "cache", "cohere", "test-slots")
	if runtime.GOOS == "darwin" {
		slots = filepath.Join(home, "Library", "Caches", "cohere", "test-slots")
	}
	tickets := func() int {
		entries, _ := os.ReadDir(queueDirectory(slots))
		count := 0
		for _, entry := range entries {
			if !strings.HasPrefix(entry.Name(), ".") {
				count++
			}
		}
		return count
	}
	waitFor := func(what string, done func() bool) {
		t.Helper()
		for deadline := time.Now().Add(time.Minute); !done(); time.Sleep(10 * time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatalf("waited a minute for %s", what)
			}
		}
	}

	// A go that records which run reached it and takes a second to test. The first run's waits for a release
	// file too, so the line forms behind it before anyone can move.
	if err := os.WriteFile(filepath.Join(bin, "go"), []byte("#!/bin/sh\necho \"$COHERE_DEV_TEST_RUN\" >> '"+order+"'\n"+
		"[ \"$COHERE_DEV_TEST_RUN\" = A ] && while [ ! -e '"+order+".release' ]; do sleep 0.05; done\nsleep 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	runs := map[string]*exec.Cmd{}
	for index, name := range []string{"A", "B", "C", "D", "E"} {
		command := exec.Command(wrapper, "test", "./...")
		command.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
			"HOME="+home, "XDG_CACHE_HOME="+filepath.Join(home, "cache"), slotsVariable+"=1", "COHERE_DEV_TEST_RUN="+name)
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
		runs[name] = command
		if index == 0 {
			waitFor("the first run to reach go", func() bool { contents, _ := os.ReadFile(order); return string(contents) == "A\n" })
		} else {
			waitFor(name+" to join the line", func() bool { return tickets() == index })
		}
	}
	if err := runs["C"].Process.Kill(); err != nil {
		t.Fatal(err)
	}
	runs["C"].Wait()
	if err := os.WriteFile(order+".release", nil, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"A", "B", "D", "E"} {
		if err := runs[name].Wait(); err != nil {
			t.Errorf("run %s failed: %v", name, err)
		}
	}
	if contents, _ := os.ReadFile(order); string(contents) != "A\nB\nD\nE\n" {
		t.Errorf("the runs reached go in the order %q, want A, B, D, E: arrival order, passing over C, which died waiting",
			strings.ReplaceAll(strings.TrimSpace(string(contents)), "\n", ", "))
	}
	if left := tickets(); left != 0 {
		t.Errorf("%d tickets were left in line after every run ended", left)
	}
}

func TestOrdinalSaysAPlaceAsAPersonWould(t *testing.T) {
	t.Parallel()
	for place, want := range map[int]string{1: "1st", 2: "2nd", 3: "3rd", 4: "4th", 11: "11th", 12: "12th", 13: "13th", 21: "21st", 112: "112th"} {
		if got := ordinal(place); got != want {
			t.Errorf("ordinal(%d) is %q, want %q", place, got, want)
		}
	}
}

// A run makes itself nice, so the go command and test binaries it starts inherit it and yield the machine
// to a person; --full-priority keeps the priority it was started with, for a timed measurement, and never
// reaches go test (#2qc6j8g). Shown at nice 19 through COHERE_DEV_NICENESS, since the house runs its tests
// at nice 10 already, where a run that failed to lower itself would look the same as one that did.
func TestARunIsNiceUnlessItAsksForFullPriority(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("nice is a Unix priority")
	}
	wrapper := filepath.Join(t.TempDir(), "cohere-dev")
	if output, err := exec.Command("go", "build", "-o", wrapper, ".").CombinedOutput(); err != nil {
		t.Fatalf("building: %v\n%s", err, output)
	}
	// A go that says how nice it is and what it was asked.
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "go"), []byte("#!/bin/sh\necho nice=$(ps -o nice= -p $$ | tr -d ' ') \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	niceOf := func(arguments ...string) (int, string) {
		t.Helper()
		command := exec.Command(wrapper, append([]string{"test"}, arguments...)...)
		command.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), nicenessVariable+"=19")
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("cohere-dev test %v: %v\n%s", arguments, err, output)
		}
		fields := strings.Fields(strings.TrimPrefix(strings.TrimSpace(string(output)), "nice="))
		nice, err := strconv.Atoi(fields[0])
		if err != nil {
			t.Fatalf("the stand-in go printed %q", output)
		}
		return nice, strings.Join(fields[1:], " ")
	}
	output, err := exec.Command("ps", "-o", "nice=", "-p", strconv.Itoa(os.Getpid())).Output()
	if err != nil {
		t.Fatal(err)
	}
	own, err := strconv.Atoi(strings.TrimSpace(string(output)))
	if err != nil {
		t.Fatalf("ps printed %q for this test's nice", output)
	}

	if own == 19 {
		t.Skip("this test already runs at nice 19, the most a run can lower itself, so lowering cannot be seen")
	}
	if nice, asked := niceOf("./internal/edit"); nice != 19 || asked != "test ./internal/edit" {
		t.Errorf("a default run's go ran at nice %d asked %q, want nice 19 asked %q", nice, asked, "test ./internal/edit")
	}
	if nice, asked := niceOf("--full-priority", "./internal/edit"); nice != own || asked != "test ./internal/edit" {
		t.Errorf("a --full-priority run's go ran at nice %d asked %q, want this test's own nice %d and the flag left out", nice, asked, own)
	}
}
