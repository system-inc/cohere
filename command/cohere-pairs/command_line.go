package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
)

// usage is said with every refusal of the command line.
const usage = "usage: cohere-pairs --base <sha> --out <directory> [--branch <sha>] [--rounds 3] [--prepare]\n" +
	"                    [--corpora <directory>] [--cohere-repository <path>] [--typescript-repository <path>]\n" +
	"                    [--adamic-repository <path>] [--loom-repository <path>]\n" +
	"from a cohere checkout every source but adamic and loom defaults to the checkout's own, and --corpora names " +
	"adamic and loom; on a tree with no .git, name every source and --branch"

// defaultRounds is how many rounds a pair runs when --rounds is not given: the three every pair has been posted with.
const defaultRounds = 3

// commandLine is what the command line asks for, its paths absolute. A source left empty takes its default from the
// checkout the command runs in or from corpora (resolveSources), and an empty branch is that checkout's `HEAD`.
type commandLine struct {
	base    string
	branch  string
	corpora string
	output  string
	rounds  int
	prepare bool

	cohereRepository     string
	typeScriptRepository string
	adamicRepository     string
	loomRepository       string
}

// parseCommandLine reads the command line, refusing one missing a flag the measurement needs.
func parseCommandLine(arguments []string) (commandLine, error) {
	flags := flag.NewFlagSet("cohere-pairs", flag.ContinueOnError)
	// The refusal is said once, by the caller, with the usage, rather than by the flag package as well.
	flags.SetOutput(io.Discard)
	asked := commandLine{}
	flags.StringVar(&asked.base, "base", "", "the commit measured against, any the cohere repository holds")
	flags.StringVar(&asked.branch, "branch", "", "the commit measured, the checkout's HEAD when it has one")
	flags.StringVar(&asked.corpora, "corpora", "", "a directory holding repositories adamic and loom")
	flags.StringVar(&asked.output, "out", "", "a new or empty directory for everything the command writes")
	flags.IntVar(&asked.rounds, "rounds", defaultRounds, "how many rounds each corpus runs")
	flags.BoolVar(&asked.prepare, "prepare", false, "fill the module cache with what measuring needs, and list it")
	flags.StringVar(&asked.cohereRepository, "cohere-repository", "", "cohere's repository, bare or not")
	flags.StringVar(&asked.typeScriptRepository, "typescript-repository", "", "the compiler's repository")
	flags.StringVar(&asked.adamicRepository, "adamic-repository", "", "adamic's repository")
	flags.StringVar(&asked.loomRepository, "loom-repository", "", "loom's repository")
	if parseError := flags.Parse(arguments); parseError != nil {
		return commandLine{}, parseError
	}
	if flags.NArg() > 0 {
		return commandLine{}, fmt.Errorf("it takes no arguments beside its flags, and was given %q", flags.Args())
	}
	switch {
	case asked.base == "":
		return commandLine{}, errors.New("--base is required")
	case asked.output == "":
		return commandLine{}, errors.New("--out is required")
	case asked.rounds < 1:
		return commandLine{}, fmt.Errorf("--rounds is %d, and a pair needs at least one", asked.rounds)
	}
	for _, path := range []*string{
		&asked.corpora,
		&asked.output,
		&asked.cohereRepository,
		&asked.typeScriptRepository,
		&asked.adamicRepository,
		&asked.loomRepository,
	} {
		if *path == "" {
			continue
		}
		absolute, absoluteError := filepath.Abs(*path)
		if absoluteError != nil {
			return commandLine{}, fmt.Errorf("resolving %s: %w", *path, absoluteError)
		}
		*path = absolute
	}
	return asked, nil
}
