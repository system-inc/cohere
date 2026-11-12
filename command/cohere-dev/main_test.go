package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/system-inc/cohere/internal/build_inputs"
	"github.com/system-inc/cohere/internal/executable_testing"
)

func TestWholeModuleAndCountOnceAreReadFromTheArguments(test *testing.T) {
	test.Parallel()
	if !wholeModule([]string{"-race", "./..."}) || wholeModule([]string{"./internal/edit"}) {
		test.Error("whole-module detection is wrong")
	}
	for _, arguments := range [][]string{{"-count=1", "./..."}, {"-count", "1", "./..."}, {"--count=1", "./..."}} {
		if !countsOnce(arguments) {
			test.Errorf("%v does not read as -count=1", arguments)
		}
	}
	if countsOnce([]string{"-count=2", "./..."}) {
		test.Error("-count=2 reads as -count=1")
	}
}

// A slot held is a slot no one else takes until it is released, and a second slot is free while the first
// is held.
func TestASlotIsHeldUntilReleased(test *testing.T) {
	test.Parallel()
	if runtime.GOOS == "windows" {
		test.Skip("slots are a Unix lock")
	}
	directory := test.TempDir()
	first, takeSlotError := takeSlot(directory, 1, "go test ./...")
	if takeSlotError != nil || first == nil {
		test.Fatalf("an empty machine gave no slot: %v", takeSlotError)
	}
	if second, takeSlotError := takeSlot(directory, 1, "go test ./..."); takeSlotError != nil || second != nil {
		test.Fatalf("a held slot was taken again (%v)", takeSlotError)
	}
	if lines := holders(directory, 1); len(lines) != 1 || !strings.Contains(lines[0], "go test ./...") {
		test.Fatalf("the holder is not named: %v", lines)
	}
	if two, takeSlotError := takeSlot(directory, 2, "go test ./..."); takeSlotError != nil || two == nil {
		test.Fatalf("with two slots the second was not free (%v)", takeSlotError)
	} else {
		two.release()
	}
	first.release()
	if again, takeSlotError := takeSlot(directory, 1, "go test ./..."); takeSlotError != nil || again == nil {
		test.Fatalf("a released slot was not free (%v)", takeSlotError)
	} else {
		again.release()
	}
}

// Two whole-module runs on a one-slot machine: the second says it is waiting and whose run holds the slot,
// and starts when the first ends. A -count=1 whole-module run is refused before anything runs.
func TestASecondWholeModuleRunWaitsAndSaysForWhom(test *testing.T) {
	test.Parallel()
	if runtime.GOOS == "windows" {
		test.Skip("slots are a Unix lock")
	}
	wrapper := filepath.Join(test.TempDir(), "cohere-dev")
	build_inputs.Record(test, ".", ".")
	if output, outputError := exec.Command("go", "build", "-o", wrapper, ".").CombinedOutput(); outputError != nil {
		test.Fatalf("building: %v\n%s", outputError, output)
	}
	// A go that takes two seconds to test anything, on the front of the path. It leaves a mark as it starts,
	// which only a run holding the slot reaches, so the second run starts once the first holds it. A fixed
	// stagger instead lost the race under the house's load, where the second run's process could start
	// first and take the slot.
	bin := test.TempDir()
	started := filepath.Join(bin, "started")
	script := "#!/bin/sh\ntouch '" + started + "'\nsleep 2\necho tested \"$@\"\n"
	if writeError := executable_testing.WriteFile(filepath.Join(bin, "go"), []byte(script), 0o755); writeError != nil {
		test.Fatal(writeError)
	}
	home := privatePool(test)
	environment := append(outsideThePool(os.Environ()), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
		homeVariable+"="+home, tokensVariable+"=1")

	run := func(arguments ...string) (string, error) {
		command := exec.Command(wrapper, append([]string{"test"}, arguments...)...)
		command.Env = environment
		output, outputError := command.CombinedOutput()
		return string(output), outputError
	}

	began := time.Now()
	outputs := make([]string, 2)
	// firstEnded closes when the first run returns: a first run that ended without leaving its mark never will,
	// so the second stops waiting on that signal rather than on a clock a loaded machine can outrun.
	firstEnded := make(chan struct{})
	var group sync.WaitGroup
	for index := range outputs {
		group.Add(1)
		go func() {
			defer group.Done()
			if index == 0 {
				defer close(firstEnded)
			}
			if index == 1 {
				for _, statError := os.Stat(started); statError != nil; _, statError = os.Stat(started) {
					select {
					case <-firstEnded:
						if _, statError := os.Stat(started); statError != nil {
							test.Errorf("the first run ended without starting to test")
							return
						}
					case <-time.After(10 * time.Millisecond):
					}
				}
			}
			output, runError := run("./...")
			if runError != nil {
				test.Errorf("run %d failed: %v\n%s", index, runError, output)
			}
			outputs[index] = output
		}()
	}
	group.Wait()
	if elapsed := time.Since(began); elapsed < 4*time.Second {
		test.Errorf("two runs on one slot finished in %s, so they overlapped", elapsed)
	}
	if !strings.Contains(outputs[1], "waiting for a token") ||
		!strings.Contains(outputs[1], "go test -vet=all ./...") ||
		!strings.Contains(outputs[1], "after waiting") {
		test.Errorf("the second run did not say it waited and for whom:\n%s", outputs[1])
	}
	if strings.Contains(outputs[0], "waiting") {
		test.Errorf("the first run waited:\n%s", outputs[0])
	}

	output, runError := run("-count=1", "./...")
	if runError == nil || !strings.Contains(output, "never passes -count=1") || strings.Contains(output, "tested") {
		test.Errorf("a -count=1 whole-module run was not refused before running:\n%s", output)
	}
	if output, runError := run(
		"-count=1",
		"./internal/edit",
	); runError != nil || !strings.Contains(output, "tested test -vet=all -count=1 ./internal/edit") {
		test.Errorf("a -count=1 run of one package was held back:\n%s (%v)", output, runError)
	}
}

// Waiters take the slot in the order they arrived, and one that dies while waiting is passed over rather
// than waited on (#nf1qj58). Five runs on a one-slot machine: the first holds the slot, three more queue
// behind it in a known order, the middle one is killed while it waits, and the rest run in arrival order.
// With the old polling, whichever waiter polled first after a release went next, so this order held by
// chance one time in six.
func TestWaitersTakeTheSlotInArrivalOrder(test *testing.T) {
	test.Parallel()
	if runtime.GOOS == "windows" {
		test.Skip("slots are a Unix lock")
	}
	wrapper := filepath.Join(test.TempDir(), "cohere-dev")
	build_inputs.Record(test, ".", ".")
	if output, outputError := exec.Command("go", "build", "-o", wrapper, ".").CombinedOutput(); outputError != nil {
		test.Fatalf("building: %v\n%s", outputError, output)
	}
	bin := test.TempDir()
	order := filepath.Join(bin, "order")
	home := privatePool(test)
	slots := filepath.Join(userCacheDirectoryIn(home), "cohere", "test-slots")
	tickets := func() int {
		entries := listMaybeMissing(test, queueDirectory(slots))
		count := 0
		for _, entry := range entries {
			if !strings.HasPrefix(entry.Name(), ".") {
				count++
			}
		}
		return count
	}
	waitFor := func(what string, done func() bool) {
		test.Helper()
		for deadline := time.Now().Add(time.Minute); !done(); time.Sleep(10 * time.Millisecond) {
			if time.Now().After(deadline) {
				test.Fatalf("waited a minute for %s", what)
			}
		}
	}

	// A go that records which run reached it and takes a second to test. The first run's waits for a release
	// file too, so the line forms behind it before anyone can move.
	if writeError := executable_testing.WriteFile(
		filepath.Join(bin, "go"),
		[]byte("#!/bin/sh\necho \"$COHERE_DEV_TEST_RUN\" >> '"+order+"'\n"+
			"[ \"$COHERE_DEV_TEST_RUN\" = A ] && while [ ! -e '"+order+".release' ]; do sleep 0.05; done\nsleep 1\n"),
		0o755,
	); writeError != nil {
		test.Fatal(writeError)
	}
	runs := map[string]*exec.Cmd{}
	for index, name := range []string{"A", "B", "C", "D", "E"} {
		command := exec.Command(wrapper, "test", "./...")
		command.Env = append(outsideThePool(os.Environ()), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
			homeVariable+"="+home, tokensVariable+"=1", "COHERE_DEV_TEST_RUN="+name)
		if startError := command.Start(); startError != nil {
			test.Fatal(startError)
		}
		runs[name] = command
		if index == 0 {
			waitFor("the first run to reach go", func() bool { return string(readMaybeMissing(test, order)) == "A\n" })
		} else {
			waitFor(name+" to join the line", func() bool { return tickets() == index })
		}
	}
	if killError := runs["C"].Process.Kill(); killError != nil {
		test.Fatal(killError)
	}
	// Ignored: C was killed on purpose, so its wait's error is that kill.
	_ = runs["C"].Wait()
	if writeError := os.WriteFile(order+".release", nil, 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	for _, name := range []string{"A", "B", "D", "E"} {
		if waitError := runs[name].Wait(); waitError != nil {
			test.Errorf("run %s failed: %v", name, waitError)
		}
	}
	if contents := readMaybeMissing(test, order); string(contents) != "A\nB\nD\nE\n" {
		test.Errorf(
			"the runs reached go in the order %q, want A, B, D, E: arrival order, passing over C, which died waiting",
			strings.ReplaceAll(strings.TrimSpace(string(contents)), "\n", ", "),
		)
	}
	if left := tickets(); left != 0 {
		test.Errorf("%d tickets were left in line after every run ended", left)
	}
}

func TestOrdinalSaysAPlaceAsAPersonWould(test *testing.T) {
	test.Parallel()
	for place, want := range map[int]string{
		1:   "1st",
		2:   "2nd",
		3:   "3rd",
		4:   "4th",
		11:  "11th",
		12:  "12th",
		13:  "13th",
		21:  "21st",
		112: "112th",
	} {
		if got := ordinal(place); got != want {
			test.Errorf("ordinal(%d) is %q, want %q", place, got, want)
		}
	}
}

// A run makes itself nice, so the go command and test binaries it starts inherit it and yield the machine
// to a person; --full-priority keeps the priority it was started with, for a timed measurement, and never
// reaches go test (#2qc6j8g). Shown at nice 19 through COHERE_DEV_NICENESS, since the house runs its tests
// at nice 10 already, where a run that failed to lower itself would look the same as one that did.
func TestARunIsNiceUnlessItAsksForFullPriority(test *testing.T) {
	test.Parallel()
	if runtime.GOOS == "windows" {
		test.Skip("nice is a Unix priority")
	}
	wrapper := filepath.Join(test.TempDir(), "cohere-dev")
	build_inputs.Record(test, ".", ".")
	if output, outputError := exec.Command("go", "build", "-o", wrapper, ".").CombinedOutput(); outputError != nil {
		test.Fatalf("building: %v\n%s", outputError, output)
	}
	// A go that says how nice it is and what it was asked.
	bin := test.TempDir()
	if writeError := executable_testing.WriteFile(
		filepath.Join(bin, "go"),
		[]byte("#!/bin/sh\necho nice=$(ps -o nice= -p $$ | tr -d ' ') \"$@\"\n"),
		0o755,
	); writeError != nil {
		test.Fatal(writeError)
	}
	// A pool of its own, so the run never waits in line behind the machine's real ones.
	home := privatePool(test)
	niceOf := func(arguments ...string) (int, string) {
		test.Helper()
		command := exec.Command(wrapper, append([]string{"test"}, arguments...)...)
		command.Env = append(outsideThePool(os.Environ()), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
			homeVariable+"="+home, nicenessVariable+"=19")
		output, outputError := command.CombinedOutput()
		if outputError != nil {
			test.Fatalf("cohere-dev test %v: %v\n%s", arguments, outputError, output)
		}
		// The stand-in's own line; the wrapper's notes, such as the corpora a private home leaves uncovered,
		// are around it.
		line := ""
		for _, candidate := range strings.Split(string(output), "\n") {
			if strings.HasPrefix(candidate, "nice=") {
				line = candidate
			}
		}
		fields := strings.Fields(strings.TrimPrefix(line, "nice="))
		if len(fields) == 0 {
			test.Fatalf("the stand-in go printed no nice line: %q", output)
		}
		nice, parseError := strconv.Atoi(fields[0])
		if parseError != nil {
			test.Fatalf("the stand-in go printed %q", output)
		}
		return nice, strings.Join(fields[1:], " ")
	}
	output, outputError := exec.Command("ps", "-o", "nice=", "-p", strconv.Itoa(os.Getpid())).Output()
	if outputError != nil {
		test.Fatal(outputError)
	}
	own, parseError := strconv.Atoi(strings.TrimSpace(string(output)))
	if parseError != nil {
		test.Fatalf("ps printed %q for this test's nice", output)
	}

	if own == 19 {
		test.Skip("this test already runs at nice 19, the most a run can lower itself, so lowering cannot be seen")
	}
	if nice, asked := niceOf("./internal/edit"); nice != 19 || asked != "test -vet=all ./internal/edit" {
		test.Errorf(
			"a default run's go ran at nice %d asked %q, want nice 19 asked %q",
			nice,
			asked,
			"test -vet=all ./internal/edit",
		)
	}
	if nice, asked := niceOf(
		"--full-priority",
		"./internal/edit",
	); nice != own || asked != "test -vet=all ./internal/edit" {
		test.Errorf(
			"a --full-priority run's go ran at nice %d asked %q, want this test's own nice %d and the flag left out",
			nice,
			asked,
			own,
		)
	}
}

// outsideThePool is an environment with no token held, for a wrapper a test starts: the test itself may run
// under a gate's token, and a wrapper that inherited it would run inside that share rather than queue.
func outsideThePool(environment []string) []string {
	kept := make([]string, 0, len(environment))
	for _, variable := range environment {
		// Land's own variables too: a test run inside a land gate would otherwise hand its fixture's landing the
		// real worktree being landed, or a stand-in meant for another test.
		if !strings.HasPrefix(variable, heldTokenVariable+"=") && !strings.HasPrefix(variable, "GOMAXPROCS=") &&
			!strings.HasPrefix(variable, "GOFLAGS=") && !strings.HasPrefix(variable, homeVariable+"=") &&
			!strings.HasPrefix(variable, landWorktreeVariable+"=") &&
			!strings.HasPrefix(variable, mainLandVariable+"=") &&
			!strings.HasPrefix(variable, gateCohereVariable+"=") &&
			!strings.HasPrefix(variable, homeGateVariable+"=") {
			kept = append(kept, variable)
		}
	}
	return kept
}

// A token's run starts its commands with Go held to the token's packages and threads, the token named, and
// the cores it is worth for a runner that is not Go,
// replacing any -p and `GOMAXPROCS` the caller had and keeping its other `GOFLAGS`, so every go command and test
// binary beneath it stays in its share (#qhg0ntb).
func TestABudgetHoldsGoToTheTokensThreads(test *testing.T) {
	test.Parallel()
	environment := budgetEnvironment(
		[]string{"HOME=/home", "GOMAXPROCS=16", "GOFLAGS=-trimpath -p=16 -buildvcs=false", heldTokenVariable + "=9"},
		2,
		poolShape{tokens: 4, packages: 3, threads: 5},
	)
	want := []string{
		"HOME=/home",
		"GOFLAGS=-trimpath -buildvcs=false -p=3",
		"GOMAXPROCS=5",
		heldTokenVariable + "=2",
		coresVariable + "=15",
	}
	if strings.Join(environment, "|") != strings.Join(want, "|") {
		test.Errorf("the budget environment is %q, want %q", environment, want)
	}
	// A caller with no `GOFLAGS`, the usual shell, still gets -trimpath: setting `GOFLAGS` overrides the
	// machine's go env, which is how every run dropped it (#system_cohere_tools).
	bare := budgetEnvironment([]string{"HOME=/home"}, 1, poolShape{tokens: 4, packages: 2, threads: 2})
	if !slices.Contains(bare, "GOFLAGS=-trimpath -p=2") {
		test.Errorf("a run from a shell with no GOFLAGS has %q, want -trimpath in it", bare)
	}
}

// A test or vet run turns VCS stamping off, which a worktree with TypeScript a symlink needs for any go build
// a test runs, adding `GOFLAGS` if there was none, and a caller's own -buildvcs stands.
func TestATestRunStampsNoVcs(test *testing.T) {
	test.Parallel()
	for _, row := range []struct {
		environment []string
		want        string
	}{
		{[]string{"HOME=/home", "GOFLAGS=-trimpath -p=2"}, "HOME=/home|GOFLAGS=-trimpath -p=2 -buildvcs=false"},
		{[]string{"HOME=/home"}, "HOME=/home|GOFLAGS=-buildvcs=false"},
		{[]string{"GOFLAGS="}, "GOFLAGS=-buildvcs=false"},
		{[]string{"GOFLAGS=-buildvcs=true -p=2"}, "GOFLAGS=-buildvcs=true -p=2"},
		{[]string{"GOFLAGS=-p=2 -buildvcs"}, "GOFLAGS=-p=2 -buildvcs"},
	} {
		if got := strings.Join(withoutVcsStamping(row.environment), "|"); got != row.want {
			test.Errorf("withoutVCSStamping(%q) is %q, want %q", row.environment, got, row.want)
		}
	}
}

// The pool runs as many commands at once as it has tokens, each held to its threads and named by its
// token, and the next waits; a command started beneath a token's run runs in that share and takes no
// second token, which would wait forever in a pool its parent fills. Shown through every verb.
func TestThePoolRunsAsManyAsItHasTokens(test *testing.T) {
	test.Parallel()
	if runtime.GOOS == "windows" {
		test.Skip("tokens are a Unix lock")
	}
	wrapper := filepath.Join(test.TempDir(), "cohere-dev")
	build_inputs.Record(test, ".", ".")
	if output, outputError := exec.Command("go", "build", "-o", wrapper, ".").CombinedOutput(); outputError != nil {
		test.Fatalf("building: %v\n%s", outputError, output)
	}
	// A go, and a tool, that log when they start and end, with their share, and take a second.
	bin := test.TempDir()
	log := filepath.Join(bin, "log")
	stand := "#!/bin/sh\necho \"start $COHERE_DEV_TEST_RUN token=$" +
		heldTokenVariable +
		" flags=$GOFLAGS threads=$GOMAXPROCS $*\" >> '" +
		log +
		"'\n" +
		"sleep 1\necho \"end $COHERE_DEV_TEST_RUN\" >> '" +
		log +
		"'\n"
	for _, name := range []string{"go", "tool"} {
		if writeError := executable_testing.WriteFile(
			filepath.Join(bin, name),
			[]byte(stand),
			0o755,
		); writeError != nil {
			test.Fatal(writeError)
		}
	}
	home := privatePool(test)
	start := func(name string, environment []string, arguments ...string) *exec.Cmd {
		command := exec.Command(wrapper, arguments...)
		command.Env = append(environment, "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
			homeVariable+"="+home, tokensVariable+"=2", packagesVariable+"=4", threadsVariable+"=3",
			"COHERE_DEV_TEST_RUN="+name)
		if startError := command.Start(); startError != nil {
			test.Fatal(startError)
		}
		return command
	}
	runs := []*exec.Cmd{
		start("test", outsideThePool(os.Environ()), "test", "./internal/edit"),
		start("build", outsideThePool(os.Environ()), "build", "./command/cohere"),
		start("vet", outsideThePool(os.Environ()), "vet", "./..."),
		start("exec", outsideThePool(os.Environ()), "exec", "--", "tool", "--flag"),
	}
	// Already under a token: runs at once, in its parent's share.
	nested := start(
		"nested",
		append(outsideThePool(os.Environ()), heldTokenVariable+"=7", "GOMAXPROCS=5", "GOFLAGS=-p=6"),
		"test",
		"./internal/edit",
	)
	for _, command := range append(runs, nested) {
		if waitError := command.Wait(); waitError != nil {
			test.Errorf("a run failed: %v", waitError)
		}
	}

	contents, readError := os.ReadFile(log)
	if readError != nil {
		test.Fatal(readError)
	}
	running, most := 0, 0
	started := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(contents)), "\n") {
		fields := strings.Fields(line)
		if fields[1] == "nested" {
			continue
		}
		if fields[0] == "start" {
			running++
			most = max(most, running)
			started[fields[1]] = strings.Join(fields[2:], " ")
		} else {
			running--
		}
	}
	if most != 2 {
		test.Errorf("at most %d ran at once on a two-token pool, want 2:\n%s", most, contents)
	}
	// test and vet run without VCS stamping (withoutVCSStamping); build and exec keep it, for a binary kept.
	for name, want := range map[string]string{
		"test":  "flags=-trimpath -p=4 -buildvcs=false threads=3 test -vet=all ./internal/edit",
		"build": "flags=-trimpath -p=4 threads=3 build ./command/cohere",
		"vet":   "flags=-trimpath -p=4 -buildvcs=false threads=3 vet ./...",
		"exec":  "flags=-trimpath -p=4 threads=3 --flag",
	} {
		if got := started[name]; !strings.HasSuffix(got, want) ||
			(!strings.HasPrefix(got, "token=1 ") && !strings.HasPrefix(got, "token=2 ")) {
			test.Errorf("%s ran with %q, want a token of the two and %q", name, got, want)
		}
	}
	if !strings.Contains(
		string(contents),
		"start nested token=7 flags=-p=6 -buildvcs=false threads=5 test -vet=all ./internal/edit",
	) {
		test.Errorf("a run already under a token did not run in its parent's share:\n%s", contents)
	}
}

// The cache trim runs beside a busy pool (#fdxencw): over its cap it takes one free token, as any heavy job does,
// and trims the least recently used entries to three quarters of the cap while another run holds a token, never
// removing an entry used within go_cache's two-hour floor, the one written a minute ago or the one ninety minutes
// ago. Then it says what it did in cohere-dev status. Before, it waited for an empty pool, which a busy day never
// gave it, and the cap never trimmed. A cache under the cap costs no token. Looks are rationed to one every ten
// minutes.
func TestTheCacheTrimRunsBesideABusyPoolAndKeepsTheFloor(test *testing.T) {
	test.Parallel()
	if runtime.GOOS == "windows" {
		test.Skip("the trim holds tokens, a Unix lock")
	}
	wrapper := filepath.Join(test.TempDir(), "cohere-dev")
	build_inputs.Record(test, ".", ".")
	if output, outputError := exec.Command("go", "build", "-o", wrapper, ".").CombinedOutput(); outputError != nil {
		test.Fatalf("building: %v\n%s", outputError, output)
	}
	home := test.TempDir()
	slots := filepath.Join(userCacheDirectoryIn(home), "cohere", "test-slots")
	if createDirectoryError := os.MkdirAll(slots, 0o755); createDirectoryError != nil {
		test.Fatal(createDirectoryError)
	}

	// A Go build cache of ten 1 KB entries three hours old, and one written a minute ago.
	// The cache in the pool home's user cache directory, where the trim guards it (poolGuards).
	cache := filepath.Join(userCacheDirectoryIn(home), "go-build")
	if createDirectoryError := os.MkdirAll(cache, 0o755); createDirectoryError != nil {
		test.Fatal(createDirectoryError)
	}
	if writeError := os.WriteFile(
		filepath.Join(cache, "README"),
		[]byte("This directory holds cached build artifacts from the Go build system.\n"),
		0o644,
	); writeError != nil {
		test.Fatal(writeError)
	}
	put := func(name string, age time.Duration) string {
		path := filepath.Join(cache, name[:2], name)
		if createDirectoryError := os.MkdirAll(filepath.Dir(path), 0o755); createDirectoryError != nil {
			test.Fatal(createDirectoryError)
		}
		if writeError := os.WriteFile(path, make([]byte, 1024), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
		when := time.Now().Add(-age)
		if changeTimesError := os.Chtimes(path, when, when); changeTimesError != nil {
			test.Fatal(changeTimesError)
		}
		return path
	}
	old := []string{}
	for index := 0; index < 10; index++ {
		old = append(
			old,
			put(fmt.Sprintf("%02x%s-d", index, strings.Repeat("a", 62)), 3*time.Hour+time.Duration(index)*time.Minute),
		)
	}
	fresh := put("a0"+strings.Repeat("a", 62)+"-a", time.Minute)
	// Used ninety minutes ago: inside the floor, though a build has had time to stop reading it.
	insideFloor := put("b0"+strings.Repeat("b", 62)+"-a", 90*time.Minute)
	// A cap of 6 KB, so the trim brings 12 KB down to three quarters of it.
	capGigabytes := strconv.FormatFloat(6*1024.0/(1<<30), 'g', -1, 64)
	environment := append(outsideThePool(os.Environ()), homeVariable+"="+home, "HOME="+scratchHome(test),
		"GOCACHE="+cache, tokensVariable+"=2", cacheCapVariable+"="+capGigabytes)

	// A run holds a token the whole time, and the trim runs beside it on the other.
	held, takeSlotError := takeSlot(slots, 2, "a run that holds a token")
	if takeSlotError != nil || held == nil || held.number != 1 {
		test.Fatalf("taking a token for the test: %v", takeSlotError)
	}
	defer held.release()
	trim := exec.Command(wrapper, trimCacheVerb)
	trim.Env = environment
	if output, trimError := trim.CombinedOutput(); trimError != nil {
		test.Fatalf("the trim failed with a token held elsewhere: %v\n%s", trimError, output)
	}
	if remaining := cacheSize(test, cache); remaining > 6*1024/4*3 {
		test.Errorf("the trim left %d bytes, over three quarters of the 6 KB cap", remaining)
	}
	if _, statError := os.Stat(fresh); statError != nil {
		test.Error("the entry written a minute ago was trimmed")
	}
	if _, statError := os.Stat(insideFloor); statError != nil {
		test.Error("the entry used ninety minutes ago, inside the floor, was trimmed")
	}
	if _, statError := os.Stat(old[9]); statError == nil {
		test.Error("the oldest entry survived the trim")
	}

	status := exec.Command(wrapper, "status")
	status.Env = environment
	output, outputError := status.CombinedOutput()
	if outputError != nil ||
		!strings.Contains(string(output), "trimmed from") ||
		!strings.Contains(string(output), "Go build cache: cap") {
		test.Errorf("cohere-dev status does not report the trim (%v):\n%s", outputError, output)
	}

	if !claimCacheLook(test.TempDir(), time.Now()) {
		test.Error("a first look was not due")
	}
	rationed := test.TempDir()
	claimCacheLook(rationed, time.Now())
	if claimCacheLook(rationed, time.Now().Add(time.Minute)) {
		test.Error("a second look a minute later was due, inside the ten minutes")
	}
}

// readMaybeMissing reads a file a run under test writes, as empty while it has not been written; any other
// failure to read it fails the test.
func readMaybeMissing(test testing.TB, path string) []byte {
	test.Helper()
	contents, readError := os.ReadFile(path)
	if readError != nil && !errors.Is(readError, fs.ErrNotExist) {
		test.Fatalf("reading %s: %v", path, readError)
	}
	return contents
}

// listMaybeMissing lists a directory a run under test makes, as empty while it has not been made; any other
// failure to list it fails the test.
func listMaybeMissing(test testing.TB, directory string) []os.DirEntry {
	test.Helper()
	entries, readDirectoryError := os.ReadDir(directory)
	if readDirectoryError != nil && !errors.Is(readDirectoryError, fs.ErrNotExist) {
		test.Fatalf("listing %s: %v", directory, readDirectoryError)
	}
	return entries
}

// cacheSize is the bytes of a test's Go build cache entries, its README left out; a cache that can't be walked
// fails the test, since its size would be wrong.
func cacheSize(test testing.TB, cache string) int64 {
	test.Helper()
	var total int64
	walkError := filepath.WalkDir(cache, func(path string, entry os.DirEntry, entryError error) error {
		if entryError != nil || entry.IsDir() || entry.Name() == "README" {
			return entryError
		}
		information, informationError := entry.Info()
		if informationError != nil {
			return informationError
		}
		total += information.Size()
		return nil
	})
	if walkError != nil {
		test.Fatalf("measuring the cache %s: %v", cache, walkError)
	}
	return total
}

// privatePool is a home of its own for the wrappers a test starts, so they queue in a pool of their own, with
// its cache already looked at, so no wrapper starts the background trim: it would run the test's stand-in go
// to find the cache.
func privatePool(test *testing.T) string {
	test.Helper()
	home := test.TempDir()
	slots := filepath.Join(userCacheDirectoryIn(home), "cohere", "test-slots")
	if createDirectoryError := os.MkdirAll(slots, 0o755); createDirectoryError != nil {
		test.Fatal(createDirectoryError)
	}
	claimCacheLook(slots, time.Now())
	return home
}

// With every token held, the trim waits its turn in line like any heavy job, touching nothing, and trims once a
// token is free (#fdxencw). It takes one token, so it never holds one idle while it waits, as the old whole-pool
// trim once held half the pool behind a land gate (2026-10-05 15:55, #jc6ca7r).
func TestTheCacheTrimWaitsItsTurnWhenEveryTokenIsHeld(test *testing.T) {
	test.Parallel()
	if runtime.GOOS == "windows" {
		test.Skip("the trim holds tokens, a Unix lock")
	}
	wrapper := filepath.Join(test.TempDir(), "cohere-dev")
	build_inputs.Record(test, ".", ".")
	if output, outputError := exec.Command("go", "build", "-o", wrapper, ".").CombinedOutput(); outputError != nil {
		test.Fatalf("building: %v\n%s", outputError, output)
	}
	home := test.TempDir()
	slots := filepath.Join(userCacheDirectoryIn(home), "cohere", "test-slots")
	if createDirectoryError := os.MkdirAll(slots, 0o755); createDirectoryError != nil {
		test.Fatal(createDirectoryError)
	}
	// The cache in the pool home's user cache directory, where the trim guards it (poolGuards).
	cache := filepath.Join(userCacheDirectoryIn(home), "go-build")
	if createDirectoryError := os.MkdirAll(cache, 0o755); createDirectoryError != nil {
		test.Fatal(createDirectoryError)
	}
	// Go's marker, without which go_cache refuses to read a directory as a cache.
	if writeError := os.WriteFile(
		filepath.Join(cache, "README"),
		[]byte("This directory holds cached build artifacts from the Go build system.\n"),
		0o644,
	); writeError != nil {
		test.Fatal(writeError)
	}
	for index := 0; index < 10; index++ {
		path := filepath.Join(
			cache,
			fmt.Sprintf("%02x", index),
			fmt.Sprintf("%02x%s-d", index, strings.Repeat("a", 62)),
		)
		if createDirectoryError := os.MkdirAll(filepath.Dir(path), 0o755); createDirectoryError != nil {
			test.Fatal(createDirectoryError)
		}
		if writeError := os.WriteFile(path, make([]byte, 1024), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
		old := time.Now().Add(-3 * time.Hour)
		if changeTimesError := os.Chtimes(path, old, old); changeTimesError != nil {
			test.Fatal(changeTimesError)
		}
	}
	first, takeSlotError := takeSlot(slots, 2, "a land gate")
	if takeSlotError != nil || first == nil {
		test.Fatalf("taking token 1: %v", takeSlotError)
	}
	second, takeSlotError := takeSlot(slots, 2, "another land gate")
	if takeSlotError != nil || second == nil {
		first.release()
		test.Fatalf("taking token 2: %v", takeSlotError)
	}
	trim := exec.Command(wrapper, trimCacheVerb)
	trim.Env = append(outsideThePool(os.Environ()), homeVariable+"="+home, "HOME="+scratchHome(test),
		"GOCACHE="+cache, tokensVariable+"=2", cacheCapVariable+"="+strconv.FormatFloat(6*1024.0/(1<<30), 'g', -1, 64))
	if startError := trim.Start(); startError != nil {
		first.release()
		second.release()
		test.Fatal(startError)
	}
	// Measuring 10 KB takes milliseconds; two seconds puts the trim well into its wait for a token.
	time.Sleep(2 * time.Second)
	if remaining := cacheSize(test, cache); remaining != 10*1024 {
		test.Errorf("the trim removed entries with every token held: %d bytes left of 10 KB", remaining)
	}
	second.release()
	waitError := trim.Wait()
	first.release()
	if waitError != nil {
		test.Fatalf("the trim failed: %v", waitError)
	}
	status := readMaybeMissing(test, filepath.Join(slots, "gocache.status"))
	if !strings.Contains(string(status), "trimmed from") {
		test.Errorf("the trim did not trim once a token was free: %q", status)
	}
}
