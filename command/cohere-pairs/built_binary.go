package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/system-inc/cohere/internal/go_engine/standard_library_table"
)

// The two sides of a pair, which name their sources, their releases and every run's record.
const (
	sideBase   = "base"
	sideBranch = "branch"
)

// builtBinary is one side's cohere, built release-shaped for the host from that side's own commit.
type builtBinary struct {
	side   string
	commit string
	path   string
	sha256 string
	// tableLine is the binary's `--version` line saying which standard library table it carries.
	tableLine string
}

// sideDirectory is where under output a side's source is checked out.
func sideDirectory(output string, side string) string {
	return filepath.Join(output, "src-"+side)
}

// sideLayers are a side's source as layers: cohere at the side's commit, and its compiler at the commit cohere pins,
// in the directory a submodule sits in, both from the private copies.
func sideLayers(checked preflight, side string) []corpusLayer {
	commit, typeScriptCommit := checked.baseCommit, checked.baseTypeScriptCommit
	if side == sideBranch {
		commit, typeScriptCommit = checked.branchCommit, checked.branchTypeScriptCommit
	}
	return []corpusLayer{
		{repository: checked.repositories.cohere, commit: commit},
		{repository: checked.repositories.typeScript, commit: typeScriptCommit, within: "TypeScript"},
	}
}

// checkOutSide checks out a side's source from the private copies, as a corpus is checked out, so the go checks before
// any build and the build itself read the same tree.
func checkOutSide(checked preflight, side string, say progress) error {
	source := sideDirectory(checked.output, side)
	for _, layer := range sideLayers(checked, side) {
		if checkOutLayerError := checkOutLayer(source, layer); checkOutLayerError != nil {
			return checkOutLayerError
		}
	}
	say.say("%s: source checked out clean in %s\n", side, source)
	return nil
}

// buildSide builds one side's cohere as a release builds it, from the side's source checkOutSide laid out, with that
// commit's own cohere-release, held off the network, and refuses a binary without its standard library table, which a
// local build lacks, so it would measure other work.
func buildSide(checked preflight, side string, say progress) (builtBinary, error) {
	source := sideDirectory(checked.output, side)
	commit := sideLayers(checked, side)[0].commit
	release := filepath.Join(checked.output, "release-"+side)
	logPath := filepath.Join(checked.output, "release-"+side+".log")
	log, createLogError := os.Create(logPath)
	if createLogError != nil {
		return builtBinary{}, fmt.Errorf("creating %s: %w", logPath, createLogError)
	}
	// Ignored: the log is only ever read by a person after a failed build, which the error below already names.
	defer func() { _ = log.Close() }()
	platform := checked.goOperatingSystem + "/" + checked.goArchitecture
	command := exec.Command(checked.goBinary, "run", "./command/cohere-release",
		"--version", "0.2.0-measure."+shortCommit(commit),
		"--only", platform,
		"--output", release)
	command.Dir = source
	// Go's version control stamp is left out of this build alone: a release stamps its provenance itself, and the
	// source is a fresh clone with another repository nested at TypeScript. `PWD` is the source's, since go names its
	// working directory by `PWD` when `PWD` is that directory, and the inherited one is the checkout's.
	command.Env = append(environmentWithout(checked.goEnvironment, "GOFLAGS", "PWD"), "GOFLAGS=-buildvcs=false",
		"PWD="+source)
	command.Stdout, command.Stderr = log, log
	say.say("%s: building cohere for %s with cohere-release, logged to %s\n", side, platform, logPath)
	if runError := command.Run(); runError != nil {
		return builtBinary{}, fmt.Errorf("cohere-release failed (%w); its output is in %s", runError, logPath)
	}

	binaryPath, findBinaryError := stagedBinary(release)
	if findBinaryError != nil {
		return builtBinary{}, findBinaryError
	}
	digest, digestError := fileSha256(binaryPath)
	if digestError != nil {
		return builtBinary{}, digestError
	}
	versionCommand := exec.Command(binaryPath, "--version")
	versionCommand.Env = environmentWithout(checked.goEnvironment, "GOFLAGS")
	versionOutput, versionError := versionCommand.CombinedOutput()
	if versionError != nil {
		return builtBinary{}, fmt.Errorf("%s --version: %w: %s", binaryPath, versionError, versionOutput)
	}
	tableLine, readTableError := releaseTableLine(string(versionOutput))
	if readTableError != nil {
		return builtBinary{}, fmt.Errorf("%s: %w", binaryPath, readTableError)
	}
	say.say("%s: %s, sha256 %s, %s\n", side, binaryPath, digest, tableLine)
	return builtBinary{side: side, commit: commit, path: binaryPath, sha256: digest, tableLine: tableLine}, nil
}

// releaseTableLine is a `--version` output's standard library table line, refusing an output whose line does not say
// the binary carries a table it reads: a binary without its table is not release-shaped, and its cold runs compute
// the standard library a release's read.
func releaseTableLine(versionOutput string) (string, error) {
	_, size, carries, says := standard_library_table.ReadVersionLine(versionOutput)
	if !says || !carries || size <= 0 {
		return "", fmt.Errorf("--version does not say it carries a standard library table it reads, so the binary "+
			"is not release-shaped:\n%s", strings.TrimSpace(versionOutput))
	}
	for line := range strings.SplitSeq(versionOutput, "\n") {
		if trimmed := strings.TrimSpace(line); strings.HasPrefix(trimmed, standard_library_table.VersionPrefix) {
			return trimmed, nil
		}
	}
	// ReadVersionLine read the line, so it is there.
	return "", fmt.Errorf("--version's table line was read and then not found:\n%s", versionOutput)
}

// stagedBinary is the one platform package's cohere a release staged under release, read from what was staged rather
// than named here, so a release that names its directories otherwise is found or refused, never guessed at. The
// dispatcher's package is named cohere and its bin/cohere is a Node script, so only the directories after it count.
func stagedBinary(release string) (string, error) {
	entries, readDirectoryError := os.ReadDir(release)
	if readDirectoryError != nil {
		return "", fmt.Errorf("reading the staged release %s: %w", release, readDirectoryError)
	}
	found := []string{}
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "cohere-") {
			continue
		}
		candidate := filepath.Join(release, entry.Name(), "bin", "cohere")
		if information, statError := os.Stat(candidate); statError == nil && information.Mode().IsRegular() {
			found = append(found, candidate)
		}
	}
	if len(found) != 1 {
		return "", fmt.Errorf("the release staged %d platform binaries under %s, and one was built: %q", len(found),
			release, found)
	}
	return found[0], nil
}

// fileSha256 is the hex sha256 of a file's contents.
func fileSha256(path string) (string, error) {
	file, openError := os.Open(path)
	if openError != nil {
		return "", openError
	}
	// Ignored: the file was only read.
	defer func() { _ = file.Close() }()
	hash := sha256.New()
	if _, copyError := io.Copy(hash, file); copyError != nil {
		return "", fmt.Errorf("reading %s: %w", path, copyError)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// environmentWithout is an environment with every variable named left out.
func environmentWithout(environment []string, names ...string) []string {
	kept := make([]string, 0, len(environment))
	for _, variable := range environment {
		name, _, _ := strings.Cut(variable, "=")
		if !slices.Contains(names, name) {
			kept = append(kept, variable)
		}
	}
	return kept
}
