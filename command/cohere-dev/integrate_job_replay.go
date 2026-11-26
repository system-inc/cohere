package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/system-inc/cohere/internal/command_line/streams"
)

// The integrate job's replay store (#etpnnw1): a check whose inputs are a passed run's needn't run again, so an
// integrate job that keeps a directory (integrateJobCacheVariable) records each pass under the key its inputs read as,
// and a later job whose key matches replays it. Keys are another hand's work, an inputKey for each step (docsDataKey,
// swiftTestsKey), so the store takes a key function of that shape, and the docs data check is wired to a placeholder
// that never keys it.

// docsDataPlaceholderKey keys the docs data check for the replay store until docsDataKey, being written beside this
// as an inputKey, is integrated and takes its place: it gives no key, so the check always runs and nothing is recorded.
func docsDataPlaceholderKey(string) (string, []string, error) {
	return "", nil, nil
}

// integrateJobReplay is the integrate job's store of steps that passed, `<directory>/steps/<step>/<key>`, each written
// only on a pass, holding what the integrate job said of it and the inputs its key read. A step whose key matches a
// record replays it rather than running, since its inputs are those of a pass; a step with no key, or a store with no
// directory, as when integrateJobCacheVariable is unset, always runs.
type integrateJobReplay struct {
	directory string
	// by names the merge a record is written for, which a replay says, and say prints the account's line of a replay
	// (integrate_job_run_account.go), nil to print none.
	by  string
	say func(line string)
}

// replayKeyPattern is a key the store files a record under: a name of letters, digits, dots, dashes and underscores,
// never a path of its own.
var replayKeyPattern = regexp.MustCompile(`^[0-9A-Za-z_][0-9A-Za-z_.-]{0,127}$`)

// run runs work, the step named step, at root, or replays a pass of it whose key, key's of root, matches, and returns
// its exit code and what the account says of it, empty when it checked nothing. A pass that checked something is
// recorded under its key; a failure never is.
func (store integrateJobReplay) run(
	step string,
	key func(root string) (string, []string, error),
	root string,
	work func() (int, string),
) (int, string) {
	if store.directory == "" {
		return work()
	}
	keyed, inputs, keyError := key(root)
	if keyError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: %s's inputs couldn't be keyed, so it runs: %v\n", step, keyError)
		return work()
	}
	if keyed == "" {
		return work()
	}
	if !replayKeyPattern.MatchString(keyed) {
		fmt.Fprintf(streams.Errors, "cohere-dev: %s's key %q names no record, so it runs\n", step, keyed)
		return work()
	}
	record := filepath.Join(store.directory, "steps", step, keyed)
	if recorded, readError := os.ReadFile(record); readError == nil {
		said, rest, _ := strings.Cut(string(recorded), "\n")
		by, _, _ := strings.Cut(rest, "\n")
		fmt.Fprintf(streams.Errors, "cohere-dev: %s replayed: its inputs are a pass's, key %s (%s)\n", step, keyed,
			record)
		if store.say != nil {
			store.say(fmt.Sprintf("skipped %s: replayed, key %s, %s", step, keyed[:min(12, len(keyed))], by))
		}
		return 0, said + ", replayed from a pass at key " + keyed[:min(12, len(keyed))]
	}
	code, said := work()
	if code != 0 || said == "" {
		return code, said
	}
	contents := said + "\nrecorded by " + shortCommit(store.by) + "\n" + strings.Join(inputs, "\n") + "\n"
	if recordError := writeReplayRecord(record, contents); recordError != nil {
		// The step passed, which is the landing's verdict; a record left unwritten only means the next job runs it.
		fmt.Fprintf(streams.Errors, "cohere-dev: %s passed, and its record couldn't be written: %v\n", step,
			recordError)
	}
	return code, said
}

// writeReplayRecord writes contents to record whole: to a file of its own beside it first, renamed over it, so a
// reader never finds half a record.
func writeReplayRecord(record string, contents string) error {
	if makeError := os.MkdirAll(filepath.Dir(record), 0o755); makeError != nil {
		return makeError
	}
	staged, createError := os.CreateTemp(filepath.Dir(record), ".record-")
	if createError != nil {
		return createError
	}
	_, writeError := staged.WriteString(contents)
	closeError := staged.Close()
	if writeError == nil {
		writeError = closeError
	}
	if writeError == nil {
		writeError = os.Rename(staged.Name(), record)
	}
	if writeError != nil {
		// Ignored: the staged file is the integrate job's own scratch, and the write's own error is the one said.
		_ = os.Remove(staged.Name())
	}
	return writeError
}
