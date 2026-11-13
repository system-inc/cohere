package main

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// neutralDirectoryName is the directory under --out the toolchains are tried and fetched in.
const neutralDirectoryName = "go-neutral"

// modulesFileName is where, in --out, --prepare writes the modules it put in the cache.
const modulesFileName = "modules.txt"

// goDirectories are the directories whose module graphs measuring reads: each side's source, then each corpus's root.
func goDirectories(checked preflight) ([]goDirectory, []goDirectory) {
	sides := []goDirectory{
		{label: sideBase, path: sideDirectory(checked.output, sideBase)},
		{label: sideBranch, path: sideDirectory(checked.output, sideBranch)},
	}
	corpora := []goDirectory{}
	for _, plannedCorpus := range checked.corpora {
		corpora = append(corpora, goDirectory{
			label: "corpus " + plannedCorpus.name,
			path:  corpusDirectory(checked.output, plannedCorpus.name),
		})
	}
	return sides, corpora
}

// makeNeutralDirectory makes the directory toolchains are tried and fetched in: a module of its own, asking for an
// old go, so neither the tree --out sits in, which may be a module, nor a go.mod above it picks the toolchain or
// takes a go.sum line.
func makeNeutralDirectory(output string) (string, error) {
	neutral := filepath.Join(output, neutralDirectoryName)
	if createDirectoryError := os.MkdirAll(neutral, 0o755); createDirectoryError != nil {
		return "", createDirectoryError
	}
	goMod := []byte("module cohere_pairs_neutral\n\ngo 1.21\n")
	if writeError := os.WriteFile(filepath.Join(neutral, "go.mod"), goMod, 0o644); writeError != nil {
		return "", writeError
	}
	return neutral, nil
}

// readyMachine checks the machine can measure with the network off, or under --prepare fills the module cache so it
// can, returning the modules it filled in, empty when checking. Either way the go commands it ran may tidy a go.sum
// in a side or a corpus, as the engine's go list does, so every changed tracked file is then put back and said.
func readyMachine(checked preflight, prepare bool, say progress) ([]string, error) {
	neutral, neutralError := makeNeutralDirectory(checked.output)
	if neutralError != nil {
		return nil, neutralError
	}
	sides, corpora := goDirectories(checked)
	prepared := []string{}
	if prepare {
		lines, prepareError := prepareModules(checked.goBinary, checked.goEnvironment, neutral, sides, corpora,
			checked.goOperatingSystem, checked.goArchitecture)
		if prepareError != nil {
			return nil, prepareError
		}
		prepared = lines
	} else if offlineError := checkOffline(
		checked.goBinary,
		checked.goEnvironment,
		neutral,
		sides,
		corpora,
		say,
	); offlineError != nil {
		return nil, offlineError
	}

	for _, side := range []string{sideBase, sideBranch} {
		putBackError := putBackTree(sideDirectory(checked.output, side), side, sideLayers(checked, side), say)
		if putBackError != nil {
			return nil, putBackError
		}
	}
	for _, plannedCorpus := range checked.corpora {
		root := corpusDirectory(checked.output, plannedCorpus.name)
		putBackError := putBackTree(root, "corpus "+plannedCorpus.name, plannedCorpus.layers, say)
		if putBackError != nil {
			return nil, putBackError
		}
	}
	if prepare {
		modulesPath := filepath.Join(checked.output, modulesFileName)
		listing := []byte(strings.Join(prepared, "\n") + "\n")
		if writeError := os.WriteFile(modulesPath, listing, 0o644); writeError != nil {
			return nil, writeError
		}
	}
	return prepared, nil
}

// putBackTree puts back every tracked file changed in each layer of the tree at root, saying each.
func putBackTree(root string, label string, layers []corpusLayer, say progress) error {
	for _, layer := range layers {
		changed, writeBackError := writeBackChangedFiles(layerDirectory(root, layer), layer.commit)
		if writeBackError != nil {
			return fmt.Errorf("%s: %w", label, writeBackError)
		}
		for _, changedPath := range changed {
			say.say("%s: go changed %s while it was checked, put back from its commit\n", label,
				path.Join(layer.within, changedPath))
		}
	}
	return nil
}
