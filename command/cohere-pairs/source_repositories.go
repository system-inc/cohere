package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// sourceRepositories are the four repositories a pair reads, by path: as the command line and the checkout name them
// before copySources, and the command's private bare copies of them after.
type sourceRepositories struct {
	cohere     string
	typeScript string
	adamic     string
	loom       string
}

// sourcesDirectoryName is the directory under --out that holds the private copies.
const sourcesDirectoryName = "sources"

// resolveSources names each source repository: the flag when it was given, and otherwise the default a house box
// has, cohere the checkout the command runs in, the compiler its TypeScript submodule's repository, and adamic and
// loom --corpora's. checkout is empty when the command runs in no checkout, as on a rented machine, whose tree came by
// git archive, and then each of its defaults is refused by the flag that replaces it.
func resolveSources(asked commandLine, checkout string) (sourceRepositories, error) {
	resolved := sourceRepositories{
		cohere:     asked.cohereRepository,
		typeScript: asked.typeScriptRepository,
		adamic:     asked.adamicRepository,
		loom:       asked.loomRepository,
	}
	missing := []error{}
	if resolved.cohere == "" {
		if checkout == "" {
			missing = append(missing, errors.New("--cohere-repository is required: the command runs in no git "+
				"checkout to read cohere from"))
		}
		resolved.cohere = checkout
	}
	if resolved.typeScript == "" {
		submodule, submoduleError := typeScriptSubmoduleRepository(checkout)
		if submoduleError != nil {
			missing = append(missing, fmt.Errorf("--typescript-repository is required: %w", submoduleError))
		}
		resolved.typeScript = submodule
	}
	for _, corpusSource := range []struct {
		path *string
		flag string
		name string
	}{
		{&resolved.adamic, "--adamic-repository", adamicCloneName},
		{&resolved.loom, "--loom-repository", loomCloneName},
	} {
		if *corpusSource.path != "" {
			continue
		}
		if asked.corpora == "" {
			missing = append(missing, fmt.Errorf("%s or --corpora is required", corpusSource.flag))
			continue
		}
		*corpusSource.path = filepath.Join(asked.corpora, corpusSource.name)
	}
	if len(missing) > 0 {
		return sourceRepositories{}, errors.Join(missing...)
	}
	return resolved, nil
}

// typeScriptSubmoduleRepository is the git directory of a checkout's TypeScript submodule. An uninitialized
// submodule is an empty directory, from which git walks up and answers for cohere itself, so the submodule's own top
// level is checked first.
func typeScriptSubmoduleRepository(checkout string) (string, error) {
	if checkout == "" {
		return "", errors.New("the command runs in no git checkout whose TypeScript submodule could be read")
	}
	submodule := filepath.Join(checkout, "TypeScript")
	topLevel, topLevelError := gitOutput(submodule, "rev-parse", "--show-toplevel")
	if topLevelError != nil || !sameDirectory(topLevel, submodule) {
		return "", fmt.Errorf("%s is not checked out. Run `git submodule update --init TypeScript`", submodule)
	}
	gitDirectory, gitDirectoryError := gitOutput(submodule, "rev-parse", "--git-dir")
	if gitDirectoryError != nil {
		return "", gitDirectoryError
	}
	if !filepath.IsAbs(gitDirectory) {
		gitDirectory = filepath.Join(submodule, gitDirectory)
	}
	return gitDirectory, nil
}

// copySources copies every source repository into output's sources directory as a bare repository this user owns, and
// returns the copies, which every later step reads instead: the sides' builds and every corpus layer. A rented
// machine's sources are bare mirrors root owns, which git refuses to open for anyone else, and a copy owned here is
// one git opens. Each copy is timed and said, since a copy of cohere with its history is not free.
func copySources(sources sourceRepositories, output string, say progress) (sourceRepositories, error) {
	directory := filepath.Join(output, sourcesDirectoryName)
	if createDirectoryError := os.MkdirAll(directory, 0o755); createDirectoryError != nil {
		return sourceRepositories{}, createDirectoryError
	}
	copies := sourceRepositories{}
	started := time.Now()
	for _, source := range []struct {
		name string
		from string
		into *string
	}{
		{"cohere", sources.cohere, &copies.cohere},
		{"typescript", sources.typeScript, &copies.typeScript},
		{"adamic", sources.adamic, &copies.adamic},
		{"loom", sources.loom, &copies.loom},
	} {
		destination, copyError := copySource(source.from, filepath.Join(directory, source.name+".git"), say)
		if copyError != nil {
			return sourceRepositories{}, copyError
		}
		*source.into = destination
	}
	say.say("sources: copied in %.1f s\n", time.Since(started).Seconds())
	return copies, nil
}

// copySource clones source, bare or not, into destination as a bare repository with objects of its own, and returns
// destination. A local clone copies the source's whole object store, so a commit no branch holds, a pin among them,
// is copied too; --no-hardlinks makes those files this user's rather than links to root's. A root-owned mirror on a
// rented machine is one git refuses to open for anyone else, "dubious ownership", unless safe.directory names it, and
// git 2.43 reads that setting for the source only from global or system configuration: `-c safe.directory` reaches
// the clone but not the upload-pack it starts in the source, which refuses (measured on Lunatic and Pupa, git 2.43.0,
// Loom jobs J-zjmdxxr, J-13pxnxg and J-0fzen3h, Nov 13). So the setting goes in a configuration file of the copy's
// own, given to the clone as GIT_CONFIG_GLOBAL, which every git the clone starts reads. That file stands in for the
// user's global configuration for this one local clone, which reads nothing else from it. It names the source and its
// .git, since git checks a bare repository at its own path and a checkout's at its .git, with every symbolic link
// resolved, as git resolves the path it compares the setting to, and macOS's /tmp is a link.
func copySource(source string, destination string, say progress) (string, error) {
	started := time.Now()
	resolved, resolveError := filepath.EvalSymlinks(source)
	if resolveError != nil {
		return "", fmt.Errorf("reading the source %s: %w", source, resolveError)
	}
	configuration := destination + ".safe-directory.gitconfig"
	writeError := os.WriteFile(configuration, []byte(safeDirectoryConfiguration(resolved)), 0o644)
	if writeError != nil {
		return "", fmt.Errorf("writing %s: %w", configuration, writeError)
	}
	_, cloneError := gitBytesIn(filepath.Dir(destination), []string{"GIT_CONFIG_GLOBAL=" + configuration}, "clone",
		"--quiet", "--bare", "--no-hardlinks", resolved, destination)
	if removeError := os.Remove(configuration); removeError != nil && cloneError == nil {
		return "", fmt.Errorf("removing %s: %w", configuration, removeError)
	}
	if cloneError != nil {
		return "", cloneError
	}
	say.say("sources: copied %s to %s in %.1f s\n", source, destination, time.Since(started).Seconds())
	return destination, nil
}

// safeDirectoryConfiguration is a git configuration naming repository, and its .git, as safe to open whoever owns it.
// A path is written as a quoted value, so a space, a semicolon or a hash in it stays part of the path.
func safeDirectoryConfiguration(repository string) string {
	quoted := func(path string) string {
		return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(path) + `"`
	}
	return "[safe]\n\tdirectory = " + quoted(repository) + "\n\tdirectory = " +
		quoted(filepath.Join(repository, ".git")) + "\n"
}
