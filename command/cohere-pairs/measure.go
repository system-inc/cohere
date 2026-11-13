package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
)

// runDeadline is how long one run of the engine may take before it is stopped and recorded as stopped. The slowest
// corpus runs in well under a minute on the machines a pair is measured on, so a run past this is stuck, not slow.
const runDeadline = 15 * time.Minute

// The directories under --out that hold each run's raw output and every .cache a cache-on run moved aside.
const (
	rawDirectoryName   = "raw"
	trashDirectoryName = "trash"
)

// measure runs the schedule, appending each run's record to runs.jsonl as it ends, and returns every record. It
// stops only when it cannot keep a record or clear a cache: a run that failed is recorded and judged by refusals.
func measure(checked preflight, base builtBinary, branch builtBinary, rounds int, say progress) ([]runRecord, error) {
	for _, directory := range []string{rawDirectoryName, trashDirectoryName} {
		createDirectoryError := os.MkdirAll(filepath.Join(checked.output, directory), 0o755)
		if createDirectoryError != nil {
			return nil, createDirectoryError
		}
	}
	runsPath := filepath.Join(checked.output, runsFileName)
	runs, openError := os.OpenFile(runsPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if openError != nil {
		return nil, openError
	}
	// Ignored: every record was written with its own call, whose failure was returned.
	defer func() { _ = runs.Close() }()

	binaries := map[string]builtBinary{sideBase: base, sideBranch: branch}
	commits := map[string]string{}
	layers := map[string][]corpusLayer{}
	for _, plannedCorpus := range checked.corpora {
		commits[plannedCorpus.name] = plannedCorpus.commit
		layers[plannedCorpus.name] = plannedCorpus.layers
	}
	trashed := 0
	records := []runRecord{}
	for _, planned := range schedule(corpusOrder, rounds) {
		corpusRoot := corpusDirectory(checked.output, planned.corpus)
		if planned.mode == modeCache {
			moved, moveError := moveCachesAside(corpusRoot, filepath.Join(checked.output, trashDirectoryName), trashed)
			if moveError != nil {
				return records, moveError
			}
			trashed += moved
		}
		rawDirectory := filepath.Join(checked.output, rawDirectoryName)
		record := runEngine(binaries[planned.side], corpusRoot, planned, rawDirectory, checked.goEnvironment)
		record.CorpusCommit = commits[planned.corpus]
		changed, restoreError := restoreChangedFiles(corpusRoot, layers[planned.corpus])
		record.ChangedFiles = changed
		records = append(records, record)
		line, encodeError := json.Marshal(record)
		if encodeError != nil {
			return records, fmt.Errorf("encoding the record of %s: %w", planned.name(), encodeError)
		}
		if _, writeError := runs.Write(append(line, '\n')); writeError != nil {
			return records, fmt.Errorf("writing the record of %s to %s: %w", planned.name(), runsPath, writeError)
		}
		// After the record is written, so what the run changed is kept even when it could not all be put back.
		if restoreError != nil {
			return records, fmt.Errorf("putting back what %s changed: %w", planned.name(), restoreError)
		}
		say.say("%s: exit %d%s, %.2f s wall, %.2f s user, %d findings, stderr %d bytes, %d tracked files changed\n",
			planned.name(), record.ExitCode, describeStopped(record), record.WallSeconds, record.UserSeconds,
			record.Findings, record.StandardErrorBytes, len(record.ChangedFiles))
		for _, changedPath := range record.ChangedFiles {
			say.say("  %s %s changed %s, %s\n", planned.corpus, planned.side, changedPath, changedFileOutcome)
		}
	}
	return records, nil
}

// runEngine runs one side's engine over a corpus and returns its record, the corpus's commit and the files it changed
// left for the caller. The engine is run as the front door runs it, marked as its child, from the corpus, in
// goEnvironment, which holds the go list it starts off the network (offlineGoEnvironment), less `GOFLAGS`, which a
// developer's run does not have, in a process group of its own, which the deadline stops whole; its stdout and stderr
// go to the raw directory, and it is timed from here: the wall by the clock, user, sys and the peak resident set from
// its rusage.
func runEngine(
	binary builtBinary,
	corpusRoot string,
	planned plannedRun,
	rawDirectory string,
	goEnvironment []string,
) runRecord {
	record := runRecord{
		Corpus:       planned.corpus,
		Side:         planned.side,
		SideCommit:   binary.commit,
		BinarySha256: binary.sha256,
		Round:        planned.round,
		Mode:         planned.mode,
		ExitCode:     -1,
	}
	if load, loadError := loadAverage(); loadError == nil {
		record.LoadBefore = &load
	}

	outputPath := filepath.Join(rawDirectory, planned.name()+".jsonl")
	errorPath := filepath.Join(rawDirectory, planned.name()+".stderr")
	output, createOutputError := os.Create(outputPath)
	if createOutputError != nil {
		record.StartError = createOutputError.Error()
		return record
	}
	// Ignored: the run's output is read back from the path below, and a close that failed loses nothing written.
	defer func() { _ = output.Close() }()
	standardError, createErrorError := os.Create(errorPath)
	if createErrorError != nil {
		record.StartError = createErrorError.Error()
		return record
	}
	// Ignored: as above.
	defer func() { _ = standardError.Close() }()

	arguments := []string{"go-engine", "--contract", "3", "--root", corpusRoot, "--no-fix", "--other-builds"}
	if planned.mode == modeNoCache {
		arguments = append(arguments, "--no-cache")
	}
	command := exec.Command(binary.path, arguments...)
	command.Dir = corpusRoot
	// `PWD` is the corpus's, as a shell's would be in it; the inherited one names where this command was started.
	command.Env = append(environmentWithout(goEnvironment, "GOFLAGS", "PWD"), "COHERE_GO_ENGINE_CHILD=1",
		"PWD="+corpusRoot)
	command.Stdout, command.Stderr = output, standardError
	command.SysProcAttr = ownProcessGroup()

	started := time.Now()
	if startError := command.Start(); startError != nil {
		record.StartError = startError.Error()
		return record
	}
	var stopped atomic.Bool
	deadline := time.AfterFunc(runDeadline, func() {
		stopped.Store(true)
		// Ignored: a group that already exited needs no stopping, and Wait below reports how its leader ended.
		_ = stopGroup(command)
	})
	// Ignored: the exit code and the rusage are read from the process state, which Wait sets however the run ended.
	_ = command.Wait()
	record.WallSeconds = time.Since(started).Seconds()
	deadline.Stop()
	record.Stopped = stopped.Load()
	record.ExitCode = command.ProcessState.ExitCode()
	record.UserSeconds = command.ProcessState.UserTime().Seconds()
	record.SystemSeconds = command.ProcessState.SystemTime().Seconds()
	record.PeakResidentBytes = peakResidentBytes(command.ProcessState)

	read, readOutputError := readRunOutput(outputPath)
	if readOutputError != nil {
		record.OutputError = readOutputError.Error()
	}
	record.engineOutput = read
	if information, statError := os.Stat(errorPath); statError == nil {
		record.StandardErrorBytes = information.Size()
	}
	return record
}

// readRunOutput reads a finished run's stdout from path.
func readRunOutput(path string) (engineOutput, error) {
	file, openError := os.Open(path)
	if openError != nil {
		return engineOutput{}, openError
	}
	// Ignored: the file was only read.
	defer func() { _ = file.Close() }()
	return readEngineOutput(file)
}

// moveCachesAside renames every .cache directory under corpusRoot into trash, each to a directory of its own numbered
// from already, and returns how many it moved. It renames and never deletes: deleting gigabytes of cache between runs
// is disk work the next run would be measured beside, and trash is --out's to clear after the pair.
func moveCachesAside(corpusRoot string, trash string, already int) (int, error) {
	found := []string{}
	walkError := filepath.WalkDir(corpusRoot, func(path string, entry fs.DirEntry, entryError error) error {
		if entryError != nil {
			return entryError
		}
		if entry.IsDir() && entry.Name() == ".cache" {
			found = append(found, path)
			return filepath.SkipDir
		}
		return nil
	})
	if walkError != nil {
		return 0, fmt.Errorf("finding the caches in %s: %w", corpusRoot, walkError)
	}
	for index, path := range found {
		relative, relativeError := filepath.Rel(corpusRoot, path)
		if relativeError != nil {
			return index, relativeError
		}
		destination := filepath.Join(trash, fmt.Sprintf("%05d-%s-%s", already+index+1, filepath.Base(corpusRoot),
			strings.ReplaceAll(filepath.ToSlash(relative), "/", "_")))
		if renameError := os.Rename(path, destination); renameError != nil {
			return index, fmt.Errorf("moving %s aside: %w", path, renameError)
		}
	}
	return len(found), nil
}

// describeStopped says, after a run's exit code, that this command stopped it.
func describeStopped(record runRecord) string {
	if record.Stopped {
		return fmt.Sprintf(", stopped after %s", runDeadline)
	}
	return ""
}
