package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// corpus is one tree the pair is measured on: the commit it is named by and every layer it is checked out from, its
// own tree first and each nested pin after the tree that pins it.
type corpus struct {
	name   string
	commit string
	layers []corpusLayer
}

// corporaDirectoryName is the directory under --out that holds every corpus.
const corporaDirectoryName = "corpora"

// planCorpora lays out every corpus in corpusOrder from the private copies, reading each nested pin from the tree
// that pins it. A pin that does not read refuses the measurement, since a corpus without it is a workspace that does
// not build.
func planCorpora(checked preflight) ([]corpus, error) {
	repositories := checked.repositories
	deeafbf4TypeScriptCommit, deeafbf4GitlinkError := gitlinkOf(repositories.cohere, deeafbf4CohereCommit,
		"TypeScript")
	if deeafbf4GitlinkError != nil {
		return nil, fmt.Errorf("corpus %s: %w", corpusDeeafbf4, deeafbf4GitlinkError)
	}
	adamicCohereCommit, adamicGitlinkError := gitlinkOf(repositories.adamic, adamicCommit, "cohere")
	if adamicGitlinkError != nil {
		return nil, fmt.Errorf("corpus %s: %w", corpusAdamic, adamicGitlinkError)
	}
	// The pinned cohere commit is read from the copy of cohere, which holds it if any repository here does.
	adamicTypeScriptCommit, adamicTypeScriptGitlinkError := gitlinkOf(repositories.cohere, adamicCohereCommit,
		"TypeScript")
	if adamicTypeScriptGitlinkError != nil {
		return nil, fmt.Errorf("corpus %s's cohere: %w", corpusAdamic, adamicTypeScriptGitlinkError)
	}

	planned := map[string]corpus{
		corpusRoot: {name: corpusRoot, commit: checked.baseCommit, layers: []corpusLayer{
			{repository: repositories.cohere, commit: checked.baseCommit},
			{repository: repositories.typeScript, commit: checked.baseTypeScriptCommit, within: "TypeScript"},
		}},
		corpusDeeafbf4: {name: corpusDeeafbf4, commit: deeafbf4CohereCommit, layers: []corpusLayer{
			{repository: repositories.cohere, commit: deeafbf4CohereCommit},
			{repository: repositories.typeScript, commit: deeafbf4TypeScriptCommit, within: "TypeScript"},
		}},
		corpusAdamic: {name: corpusAdamic, commit: adamicCommit, layers: []corpusLayer{
			{repository: repositories.adamic, commit: adamicCommit},
			{repository: repositories.cohere, commit: adamicCohereCommit, within: "cohere"},
			{repository: repositories.typeScript, commit: adamicTypeScriptCommit, within: "cohere/TypeScript"},
		}},
		corpusLoom: {name: corpusLoom, commit: loomCommit, layers: []corpusLayer{
			{repository: repositories.loom, commit: loomCommit},
		}},
	}
	corpora := make([]corpus, 0, len(corpusOrder))
	for _, name := range corpusOrder {
		corpora = append(corpora, planned[name])
	}
	return corpora, nil
}

// corpusDirectory is where under output a corpus is checked out.
func corpusDirectory(output string, name string) string {
	return filepath.Join(output, corporaDirectoryName, name)
}

// layerDirectory is where in a corpus a layer is checked out.
func layerDirectory(corpusRoot string, layer corpusLayer) string {
	return filepath.Join(corpusRoot, filepath.FromSlash(layer.within))
}

// prepareCorpus checks out every layer of a corpus as a repository of its own at its pin, each nested layer cloned into
// the directory its gitlink names inside the checkout above it, as an initialized submodule sits. A corpus has to be a
// checkout: the engine reads a submodule as a repository apart and leaves its files out, and a tree with no `.git`
// has no boundaries, so cohere's root counted its TypeScript/tsc module, 5,139 files, as its own, and adamic read
// 126,043 findings against a checkout's 67,685 (#j9e4ejd, Pupa's first run). No layer is reached through a symbolic
// link, since a root reached through one checks 0 files (#yyv7nr2). Each layer's status is held empty right after its
// checkout, so a corpus that is not its commit is refused before it is measured.
func prepareCorpus(output string, plannedCorpus corpus, say progress) error {
	root := corpusDirectory(output, plannedCorpus.name)
	for _, layer := range plannedCorpus.layers {
		if checkOutLayerError := checkOutLayer(root, layer); checkOutLayerError != nil {
			return checkOutLayerError
		}
	}
	say.say("corpus %s: %d repositories checked out clean in %s\n", plannedCorpus.name, len(plannedCorpus.layers),
		root)
	return nil
}

// checkOutLayer clones a layer's repository into its directory in the corpus, sharing its objects, checks out its pin
// detached, and refuses a checkout whose status is not empty. The directory is new, or the empty one the checkout
// above it left for its gitlink.
func checkOutLayer(corpusRoot string, layer corpusLayer) error {
	destination := layerDirectory(corpusRoot, layer)
	if createParentError := os.MkdirAll(filepath.Dir(destination), 0o755); createParentError != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(destination), createParentError)
	}
	if _, cloneError := gitOutput(filepath.Dir(destination), "clone", "--quiet", "--shared", "--no-checkout",
		layer.repository, destination); cloneError != nil {
		return cloneError
	}
	_, checkoutError := gitOutput(destination, "checkout", "--quiet", "--detach", layer.commit)
	if checkoutError != nil {
		return checkoutError
	}
	status, statusError := gitOutput(destination, "status", "--porcelain")
	if statusError != nil {
		return statusError
	}
	if status != "" {
		return fmt.Errorf("%s checked out at %s and its status is not empty:\n%s", destination, layer.commit, status)
	}
	return nil
}
