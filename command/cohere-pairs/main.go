// Command cohere-pairs measures one pair: the Go engine of a base commit against the Go engine of a branch commit,
// over the corpora every pair since 2026-11-10 was measured on, and prints the table a pair is posted with (#j9e4ejd).
// It is one command so a Loom job, which lays a cohere commit on a machine and runs one command there, can measure a
// pair with nothing else running. It runs the same on two roads:
//
//	# A house box (Pupa, Home): from a cohere checkout, with clones adamic and loom under one directory.
//	GOWORK=off go build -buildvcs=false -o cohere-pairs ./command/cohere-pairs
//	./cohere-pairs --prepare --base <sha> --corpora <directory> --out <new directory>
//	./cohere-pairs --base <sha> --corpora <directory> --out <new directory> [--rounds 3]
//
//	# A sealed rented machine: the tree came by git archive, with no .git, the network is off, and the sources are
//	# bare mirrors under /var/lib/loom/mirrors. --prepare runs where the network is on, into the image's GOMODCACHE.
//	GOWORK=off go build -buildvcs=false -o cohere-pairs ./command/cohere-pairs
//	sources="--cohere-repository <mirror> --typescript-repository <mirror> --adamic-repository <mirror> \
//	    --loom-repository <mirror>"
//	./cohere-pairs --prepare --base <sha> --branch <sha> --out <new directory> $sources
//	./cohere-pairs --base <sha> --branch <sha> --out <new directory> $sources [--rounds 3]
//
// A job builds it and runs the binary, rather than `go run` it, since `go run` turns every exit but 0 into 1, and
// the exit code is how a job tells a pair that could not be measured from one measured and refused. It builds with
// `GOWORK=off`, from cohere's go.mod alone, since a tree laid by git archive holds no TypeScript submodule for go.work
// to use, and the command needs none; and with `-buildvcs=false`, since such a tree has no .git to stamp from. Its
// modules are the branch's, which --prepare puts in the cache.
//
// The flags:
//
//	--base <sha>                     the commit measured against, required
//	--branch <sha>                   the commit measured; the checkout's HEAD by default, which must then be clean,
//	                                 and required where the command runs in no checkout
//	--out <directory>                new or empty; everything the command writes goes here
//	--rounds <n>                     rounds per corpus, 3 by default
//	--prepare                        fill the module cache and list it, then exit 0 without measuring
//	--cohere-repository <path>       cohere, bare or not; the checkout the command runs in by default
//	--typescript-repository <path>   the compiler; that checkout's TypeScript submodule by default
//	--adamic-repository <path>       adamic; --corpora's adamic by default
//	--loom-repository <path>         loom; --corpora's loom by default
//	--corpora <directory>            the shorthand naming adamic and loom at once
//
// Every source is first copied into --out as a bare repository this user owns (copySources), so a root-owned mirror
// is read, and every build and corpus reads only those copies. The sources are read, never changed, and the commits
// must be in them: both sides, the compiler each pins, and the corpus pins (corpus_pins.go).
//
// Measuring never reaches the network: every go command it starts, both release builds, the standard library tables
// they generate and each engine run's go list, runs with `GOPROXY=off` and no checksum database
// (offlineGoEnvironment). So the machine must hold, before it measures, the go each side's go.mod asks for, every
// toolchain the release's tables are generated under, and the module graph of each side and each corpus; the preflight
// checks each, and refuses naming what is missing. --prepare, run with the network and the same flags into the same
// `GOMODCACHE`, puts all of it in the module cache and lists it, one "path version" a line, toolchains included, for an
// image to be baked from. Go's build cache must be writable, as for any go build. On darwin cohere-release also builds
// the Swift engine, so a Mac needs Swift, and SwiftPM fetches its packages unless its scratch already holds them.
//
// --out holds:
//
//	sources/<name>.git            the private copies of the sources
//	src-base, src-branch          each side's source, checked out from the copies
//	go-neutral                    where the toolchains are tried and fetched
//	modules.txt                   what --prepare put in the module cache
//	release-base, release-branch  each side's release-shaped package for this host, built by its own cohere-release
//	release-<side>.log            what that release build printed
//	corpora/<name>                each corpus checked out at its pin, a nested pin a repository inside it
//	trash                         every .cache directory a cache-on run found, moved aside rather than deleted
//	raw/<corpus>-<side>-<round>-<mode>.jsonl and .stderr   what each run of the engine printed
//	runs.jsonl                    one record per run (runRecord)
//	table.md                      the table, also printed, written only when every check holds
//
// Each side is built as a release is, with the standard library's table in the binary, since a local build computes
// the standard library on every cold run and measures other work (#ccqee0e).
//
// Its exit code says which of three things happened:
//
//	0  every check held, and the table was printed and written to table.md; or --prepare filled the cache
//	1  runs were measured and it refuses to report them, naming each reason (refusals.go), or the measurement
//	   stopped partway, as when a file a run changed could not be put back; every record taken is in runs.jsonl
//	2  nothing was measured: the command line or a preflight check was refused, the machine lacks what measuring
//	   offline needs, a side did not build, or a corpus did not check out clean
//
// A tracked file a run changes, as go list tidies a go.work.sum, is put back from its commit before the next run
// and counted in the record and the table (changed_files.go); that refuses nothing.
package main

import (
	"io"
	"os"
	"path/filepath"
)

// The exit codes: the table was printed; runs were measured and the command refuses to report them; it refused
// before measuring anything.
const (
	exitReported         = 0
	exitRefusedToReport  = 1
	exitRefusedToMeasure = 2
)

// runsFileName and tableFileName are where, in --out, the records and the table are written.
const (
	runsFileName  = "runs.jsonl"
	tableFileName = "table.md"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run measures the pair the command line asks for, printing its progress and table to standardOutput and what
// stopped it to standardError, and returns the exit code.
func run(arguments []string, standardOutput io.Writer, standardError io.Writer) int {
	say := progress{writer: standardOutput}
	complain := progress{writer: standardError}
	asked, parseError := parseCommandLine(arguments)
	if parseError != nil {
		complain.say("cohere-pairs: %v\n%s\n", parseError, usage)
		return exitRefusedToMeasure
	}
	checked, preflightError := runPreflight(asked, say)
	if preflightError != nil {
		complain.say("cohere-pairs: refused before measuring: %v\n", preflightError)
		return exitRefusedToMeasure
	}
	for _, side := range []string{sideBase, sideBranch} {
		if checkOutSideError := checkOutSide(checked, side, say); checkOutSideError != nil {
			complain.say("cohere-pairs: refused before measuring: checking out %s: %v\n", side, checkOutSideError)
			return exitRefusedToMeasure
		}
	}
	for _, plannedCorpus := range checked.corpora {
		if prepareCorpusError := prepareCorpus(checked.output, plannedCorpus, say); prepareCorpusError != nil {
			complain.say("cohere-pairs: refused before measuring: preparing corpus %s: %v\n",
				plannedCorpus.name, prepareCorpusError)
			return exitRefusedToMeasure
		}
	}

	prepared, readyError := readyMachine(checked, asked.prepare, say)
	if readyError != nil {
		complain.say("cohere-pairs: refused before measuring: %v\n", readyError)
		return exitRefusedToMeasure
	}
	if asked.prepare {
		say.say("prepared: %d modules in the module cache, listed in %s:\n", len(prepared),
			filepath.Join(checked.output, modulesFileName))
		for _, line := range prepared {
			say.say("%s\n", line)
		}
		return exitReported
	}

	base, buildBaseError := buildSide(checked, sideBase, say)
	if buildBaseError != nil {
		complain.say("cohere-pairs: refused before measuring: building base: %v\n", buildBaseError)
		return exitRefusedToMeasure
	}
	branch, buildBranchError := buildSide(checked, sideBranch, say)
	if buildBranchError != nil {
		complain.say("cohere-pairs: refused before measuring: building branch: %v\n", buildBranchError)
		return exitRefusedToMeasure
	}

	records, measureError := measure(checked, base, branch, asked.rounds, say)
	if measureError != nil {
		complain.say("cohere-pairs: refused to report, the measurement stopped: %v\n"+
			"every run measured before it is in %s\n", measureError, filepath.Join(checked.output, runsFileName))
		return exitRefusedToReport
	}

	if failures := refusals(records); len(failures) > 0 {
		complain.say("cohere-pairs: refused to report, %d checks failed (records in %s):\n",
			len(failures), filepath.Join(checked.output, runsFileName))
		for _, failure := range failures {
			complain.say("  %s\n", failure)
		}
		return exitRefusedToReport
	}

	table := renderTable(tableHeader{
		base:              base,
		branch:            branch,
		host:              checked.host,
		goVersion:         checked.goVersion,
		goOperatingSystem: checked.goOperatingSystem,
		goArchitecture:    checked.goArchitecture,
		cores:             checked.cores,
		rounds:            asked.rounds,
	}, records)
	tablePath := filepath.Join(checked.output, tableFileName)
	if writeError := os.WriteFile(tablePath, []byte(table), 0o644); writeError != nil {
		complain.say("cohere-pairs: writing %s: %v\n", tablePath, writeError)
		return exitRefusedToReport
	}
	say.say("\n%s", table)
	return exitReported
}
