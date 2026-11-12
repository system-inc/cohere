package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/system-inc/cohere/internal/build_inputs"
)

// A quiet window refuses every job that starts inside it but the run it is for, and says how long and how to
// wait (#1rh8dav): one with no name, or another name, is refused; the named run is let through; a job beneath
// a running job's token runs in that share; and a window that has expired, or none, refuses nothing. A marker
// that can't be read holds, as it holds the trim.
func TestAQuietWindowRefusesEveryJobButTheRunItIsFor(test *testing.T) {
	test.Parallel()
	directory := test.TempDir()
	opened := time.Date(2026, 11, 11, 20, 5, 0, 0, time.Local)
	if refusal := quietRefusal(directory, opened, "", "", discarded()); refusal != "" {
		test.Errorf("with no window a job was refused: %s", refusal)
	}
	if code := quietOn(
		directory,
		[]string{"--for", "45m", "--through", "engine-pairs", "engine's timing pairs"},
		opened,
		discarded(),
		discarded(),
	); code != 0 {
		test.Fatalf("opening a quiet window exited %d", code)
	}
	inside := opened.Add(10 * time.Minute)
	for _, through := range []string{"", "staticcheck-census"} {
		refusal := quietRefusal(directory, inside, "", through, discarded())
		for _, want := range []string{
			"holds until 20:50:00",
			"engine's timing pairs",
			"no test, build, vet, exec or land starts",
			"only the run it is for, --through engine-pairs",
			"quiet wait",
		} {
			if !strings.Contains(refusal, want) {
				test.Errorf("a job named %q inside the window was refused without %q: %q", through, want, refusal)
			}
		}
	}
	if refusal := quietRefusal(directory, inside, "", "engine-pairs", discarded()); refusal != "" {
		test.Errorf("the run the window is for was refused: %s", refusal)
	}
	if refusal := quietRefusal(directory, inside, "3", "", discarded()); refusal != "" {
		test.Errorf("a job beneath a running job's token was refused: %s", refusal)
	}
	if refusal := quietRefusal(directory, opened.Add(46*time.Minute), "", "", discarded()); refusal != "" {
		test.Errorf("a job after the window expired was refused: %s", refusal)
	}

	unnamed := test.TempDir()
	if code := quietOn(unnamed, []string{"--for", "10m"}, opened, discarded(), discarded()); code != 0 {
		test.Fatalf("opening an unnamed quiet window exited %d", code)
	}
	if refusal := quietRefusal(
		unnamed,
		inside.Add(-5*time.Minute),
		"",
		"engine-pairs",
		discarded(),
	); !strings.Contains(refusal, "it lets no run through") {
		test.Errorf("a window naming no run let a named job through, or did not say it lets none: %q", refusal)
	}
	if writeError := os.WriteFile(quietPath(unnamed), []byte("tomorrow\n"), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	if refusal := quietRefusal(
		unnamed,
		inside,
		"",
		"",
		discarded(),
	); !strings.Contains(refusal, "does not start with when it ends") {
		test.Errorf("a marker that can't be read refused nothing, or did not say why: %q", refusal)
	}
	if code := quietOn(
		test.TempDir(),
		[]string{"--through", "two words"},
		opened,
		discarded(),
		discarded(),
	); code != 2 {
		test.Errorf("a --through of two words was not refused: exited %d", code)
	}
}

// The --through flag is read wherever go's flags are and only before exec's command, and never reaches go.
func TestTheThroughFlagIsReadBeforeTheCommandsOwn(test *testing.T) {
	test.Parallel()
	for _, row := range []struct {
		arguments []string
		leading   bool
		kept      string
		through   string
	}{
		{[]string{"-run", "X", "./internal/edit"}, false, "-run X ./internal/edit", ""},
		{[]string{"-run", "X", "--through", "pairs", "./internal/edit"}, false, "-run X ./internal/edit", "pairs"},
		{[]string{"--through=pairs", "./..."}, false, "./...", "pairs"},
		{[]string{"./internal/edit", "-args", "--through", "x"}, false, "./internal/edit -args --through x", ""},
		{[]string{"--through", "pairs", "--", "bench", "--through", "x"}, true, "-- bench --through x", "pairs"},
		{[]string{"--bounded", "--through", "pairs", "bench"}, true, "--bounded bench", "pairs"},
		{[]string{"bench", "--through", "x"}, true, "bench --through x", ""},
	} {
		kept, through, readError := withoutThroughFlag(row.arguments, row.leading)
		if readError != nil {
			test.Errorf("%q: %v", row.arguments, readError)
			continue
		}
		if strings.Join(kept, " ") != row.kept || through != row.through {
			test.Errorf("%q read as %q through %q, want %q through %q",
				row.arguments, kept, through, row.kept, row.through)
		}
	}
	if _, _, readError := withoutThroughFlag([]string{"--through"}, false); readError == nil {
		test.Error("a --through with no name was not refused")
	}
}

// Nothing is borrowed inside a quiet window, so a job running when it opened keeps its width; once the window
// is released the same pool lends again.
func TestNothingIsBorrowedInsideAQuietWindow(test *testing.T) {
	test.Parallel()
	if runtime.GOOS == "windows" {
		test.Skip("tokens are a Unix lock")
	}
	directory := test.TempDir()
	shape := poolShape{tokens: 4, packages: 2, threads: 2}
	own, takeSlotError := takeSlot(directory, shape.tokens, "a gate running when the window opened")
	if takeSlotError != nil || own == nil {
		test.Fatalf("taking a token for the test: %v", takeSlotError)
	}
	defer own.release()
	if code := quietOn(
		directory,
		[]string{"--for", "10m", "a measurement"},
		time.Now(),
		discarded(),
		discarded(),
	); code != 0 {
		test.Fatalf("opening a quiet window exited %d", code)
	}
	if borrowed := borrowTokens(directory, shape, own.number, "a gate"); len(borrowed) != 0 {
		for _, token := range borrowed {
			token.release()
		}
		test.Errorf("a run borrowed %d idle tokens inside a quiet window", len(borrowed))
	}
	if removeError := os.Remove(quietPath(directory)); removeError != nil {
		test.Fatal(removeError)
	}
	borrowed := borrowTokens(directory, shape, own.number, "a gate")
	for _, token := range borrowed {
		token.release()
	}
	if len(borrowed) != 2 {
		test.Errorf("with the window released a run on an idle pool of 4 borrowed %d tokens, want 2", len(borrowed))
	}
}

// The wall end to end, through a built cohere-dev on a pool of its own: inside a window a job is refused
// before it runs anything, with the exit code that says try again later; the run the window is for runs,
// and what it starts carries its name; status names the job already running inside the window; and a job
// that was waiting in line when the window opened is refused when it takes its token, since waiting is not
// running.
func TestAQuietWindowIsAWallWhereJobsStart(test *testing.T) {
	test.Parallel()
	if runtime.GOOS == "windows" {
		test.Skip("tokens are a Unix lock")
	}
	wrapper := filepath.Join(test.TempDir(), "cohere-dev")
	build_inputs.Record(test, ".", ".")
	if output, outputError := exec.Command("go", "build", "-o", wrapper, ".").CombinedOutput(); outputError != nil {
		test.Fatalf("building: %v\n%s", outputError, output)
	}
	home := privatePool(test)
	slots := filepath.Join(userCacheDirectoryIn(home), "cohere", "test-slots")
	environment := append(outsideThePool(os.Environ()), homeVariable+"="+home, tokensVariable+"=1")
	run := func(arguments ...string) (string, int) {
		test.Helper()
		command := exec.Command(wrapper, arguments...)
		command.Env = environment
		output, runError := command.CombinedOutput()
		var exited *exec.ExitError
		if runError != nil && !errors.As(runError, &exited) {
			test.Fatalf("cohere-dev %s: %v\n%s", strings.Join(arguments, " "), runError, output)
		}
		code := 0
		if exited != nil {
			code = exited.ExitCode()
		}
		return string(output), code
	}
	ran := filepath.Join(test.TempDir(), "ran")
	marking := []string{"--", "sh", "-c", "echo \"through=$" + quietThroughVariable + "\" >> '" + ran + "'"}

	// A job in line when the window opens: the one token is held, so it waits, and is refused once it takes it.
	held, takeSlotError := takeSlot(slots, 1, "a gate running when the window opened")
	if takeSlotError != nil || held == nil {
		test.Fatalf("taking the token for the test: %v", takeSlotError)
	}
	waiting := exec.Command(wrapper, append([]string{"exec"}, marking...)...)
	waiting.Env = environment
	var waitingOutput strings.Builder
	waiting.Stdout, waiting.Stderr = &waitingOutput, &waitingOutput
	if startError := waiting.Start(); startError != nil {
		held.release()
		test.Fatal(startError)
	}
	for deadline := time.Now().Add(time.Minute); len(waiters(slots)) == 0; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			held.release()
			// Ignored: the job is stopped because the test is failing; its error says nothing more.
			_ = waiting.Process.Kill()
			test.Fatal("waited a minute for the job to join the line")
		}
	}
	if output, code := run("quiet", "on", "--for", "10m", "--through", "engine-pairs", "engine's pairs"); code != 0 {
		held.release()
		test.Fatalf("quiet on exited %d:\n%s", code, output)
	}
	status, _ := run("status")
	var exited *exec.ExitError
	// Refused where it starts, before it joins the line: with the one token held, a job the wall did not stop
	// would wait in line for it.
	early := exec.Command(wrapper, append([]string{"exec"}, marking...)...)
	early.Env = environment
	early.WaitDelay = time.Second
	timer := time.AfterFunc(30*time.Second, func() {
		// Ignored: a job still waiting after 30s is stopped so the test can say so; its error is that stop.
		_ = early.Process.Kill()
	})
	earlyOutput, earlyError := early.CombinedOutput()
	timer.Stop()
	if !errors.As(earlyError, &exited) || exited.ExitCode() != quietRefusedExitCode ||
		strings.Contains(string(earlyOutput), "waiting for a token") {
		test.Errorf("a job inside the window ended with %v, want exit %d before joining the line:\n%s",
			earlyError, quietRefusedExitCode, earlyOutput)
	}
	held.release()
	waitError := waiting.Wait()
	if !errors.As(waitError, &exited) || exited.ExitCode() != quietRefusedExitCode ||
		!strings.Contains(waitingOutput.String(), "no test, build, vet, exec or land starts") {
		test.Errorf("a job waiting in line when the window opened ended with %v, want exit %d and the window "+
			"named:\n%s", waitError, quietRefusedExitCode, waitingOutput.String())
	}
	if !strings.Contains(status, "a gate running when the window opened (running inside the quiet window)") ||
		!strings.Contains(status, "no job starts but --through engine-pairs") {
		test.Errorf("status does not name the job running inside the window, or the run it lets through:\n%s", status)
	}

	if output, code := run(append([]string{"exec"}, marking...)...); code != quietRefusedExitCode ||
		!strings.Contains(output, "quiet wait") {
		test.Errorf("a job inside the window exited %d, want %d with how to wait:\n%s",
			code, quietRefusedExitCode, output)
	}
	if output, code := run(
		append([]string{"exec", "--through", "other-run"}, marking...)...,
	); code != quietRefusedExitCode {
		test.Errorf("a job naming another run exited %d, want %d:\n%s", code, quietRefusedExitCode, output)
	}
	if output, code := run(append([]string{"exec", "--through", "engine-pairs"}, marking...)...); code != 0 {
		test.Errorf("the run the window is for exited %d:\n%s", code, output)
	}
	if marks := string(readMaybeMissing(test, ran)); marks != "through=engine-pairs\n" {
		test.Errorf("what ran inside the window was %q, want only the run it is for, carrying its name", marks)
	}

	if output, code := run("quiet", "off"); code != 0 {
		test.Fatalf("quiet off exited %d:\n%s", code, output)
	}
	if output, code := run(append([]string{"exec"}, marking...)...); code != 0 {
		test.Errorf("with the window released a job exited %d:\n%s", code, output)
	}
}
