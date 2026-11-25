package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/system-inc/cohere/internal/testing/corpus"
)

// The private corpora on the gate's Macs (#5r0vrte). Some tests read code outside this repository
// (internal/testing/corpus), and a gate box held none of it, so 202 of them skipped there and ran nowhere. Now each Mac
// keeps every corpus laid read-only under macBoxScratch/corpora/<name>/<commit>, at the commits
// internal/testing/corpus/corpus_pinned.go names in the tree being landed, and the gate's script sets each corpus's
// variable to its laid copy before the tests.
//
// A corpus is laid from the lander's own checkout, the one its corpora file names, and reaches the box only as a
// carry on the gap that lays it: Loom's door never places a job that reads it, so ahra's source goes to the named
// Macs alone. The script names what its box lacks (corpusUnlaidMarker) and tests nothing; the lander then lays those
// corpora on that box in a gap of their own, and asks again. A corpus is laid once per commit per box, so a landing
// pays only when a pin moves. What a commit of a corpus is laid as:
//
//   - ahra: its tree without data/, which holds Kirk's conversations and which no test reads; each submodule filled in
//     at the commit the tree records, from the checkout's own submodule repository; then its lockfile installed, frozen
//     and with no install scripts, since the tests read installed packages (tailwindcss, @types/react).
//   - connected: the same, nothing left out.
//   - the Prettier fork: its tree, then its yarn.lock installed by the yarn it vendors, since its oracles run node
//     inside it, and the eight bundles cohere embeds built into its dist/, since with the fork set cohere loads
//     its bundles from there (internal/format/prettier's Bundles). Built at the pin on Sun2 they matched the embedded
//     ones byte for byte (Nov 16); the fork's whole build doesn't finish on a box, since its oxc plugin runs a yarn
//     of its own, and none of the eight needs it.
//   - Structure: a repository holding every commit a test reads through At, checked out at its checkout pin, since its
//     tests read commits by git as well as the tree on disk.

// corpusUnlaidMarker opens the line the gate's Mac script prints, in place of testing, when its box lacks a corpus
// the landing pins: the places it lacks follow, each `<name>/<commit>`.
const corpusUnlaidMarker = "cohere-dev-gate: unlaid "

// corpusLaidMarker opens each line the lay script prints for a corpus it laid.
const corpusLaidMarker = "cohere-dev-gate: laid "

// corpusCarryLimit is the most a gap carries in one file (LoomGapCommandLineInterface.ts's --carry).
const corpusCarryLimit = 256 << 20

// corpusLayBound is the lay gap's --within: an ahra install took 24s on Sun2 (Nov 16), and a cold one fetches every
// package, so a half hour leaves it room while one that hangs still gives the box back.
const corpusLayBound = "30m"

// corpusFreeFloor is how many GiB, as the script spells it, a box keeps free after laying: Sun2's disk was 97% used
// with 28 GiB free (Nov 16), so a lay that would leave less refuses rather than filling the disk every org on her
// shares.
const corpusFreeFloor = "8"

// corpusForkBundleFlags is the fork build's --file for each bundle cohere embeds, internal/format/prettier's
// BundleFiles under dist/prettier, which a test holds to that list.
const corpusForkBundleFlags = " --file prettier/standalone.js --file prettier/plugins/estree.js" +
	" --file prettier/plugins/typescript.js --file prettier/plugins/babel.js --file prettier/plugins/postcss.js" +
	" --file prettier/plugins/markdown.js --file prettier/plugins/graphql.js --file prettier/plugins/yaml.js"

// corpusRecipe is how a corpus is laid: excluded is what of its tree never leaves the lander, install is how the box
// installs its packages (corpusInstallNone, corpusInstallPnpm, corpusInstallFork), and repository is whether it is laid
// as a git repository holding every commit a test reads through At rather than as a tree.
type corpusRecipe struct {
	excluded   []string
	install    string
	repository bool
}

// The lay script's installs.
const (
	corpusInstallNone = "none"
	corpusInstallPnpm = "pnpm"
	corpusInstallFork = "fork"
)

var corpusRecipes = map[string]corpusRecipe{
	corpus.Ahra.Name:         {excluded: []string{"data"}, install: corpusInstallPnpm},
	corpus.Connected.Name:    {install: corpusInstallPnpm},
	corpus.PrettierFork.Name: {install: corpusInstallFork},
	corpus.Structure.Name:    {install: corpusInstallNone, repository: true},
}

// gateCorpus is one corpus a landing lays: its pins, read from the tree being landed, and the lander's checkout of it.
type gateCorpus struct {
	corpus   corpus.Corpus
	pins     corpus.Pins
	checkout string
}

// place is where the corpus is laid on a box, under macBoxScratch/corpora: its name, then its checkout commit with the
// digest of how it is laid (corpusRecipeDigest), so a lay that changes lays the corpus again rather than trusting a
// copy laid the old way.
func (gate gateCorpus) place() string {
	return gate.corpus.Name + "/" + gate.pins.Checkout + "-" + corpusRecipeDigest(gate.corpus.Name)
}

// gateCorpora is every corpus the landing of commit lays, and a line for each it can't, naming why: the commit names
// no pins, which only a tree from before them does, or the lander's corpora file names no checkout to lay it from.
// Those run nowhere, named on the land line, as every corpus did before #5r0vrte.
func gateCorpora(worktree string, commit string) ([]gateCorpus, []string, error) {
	source, showError := gitOutput(worktree, "show", commit+":"+corpus.PinnedFile)
	if showError != nil {
		return nil, []string{fmt.Sprintf("no corpus is laid, since %s has no %s, so the tests that read one run "+
			"nowhere", shortCommit(commit), corpus.PinnedFile)}, nil
	}
	pins, pinsError := corpus.PinsIn([]byte(source))
	if pinsError != nil {
		return nil, nil, pinsError
	}
	path, pathError := corpus.ConfigPath()
	if pathError != nil {
		return nil, nil, pathError
	}
	configuration, configurationError := corpus.ReadConfiguration(path)
	if configurationError != nil {
		return nil, nil, configurationError
	}
	laid := []gateCorpus{}
	unlaid := []string{}
	for _, each := range corpus.All {
		checkout := os.Getenv(each.Variable)
		if checkout == "" {
			checkout = configuration[each.Name]
		}
		if checkout == "" {
			unlaid = append(unlaid, fmt.Sprintf("%s isn't laid, since %s names no checkout of it to lay from, so "+
				"its tests run nowhere", each.Name, path))
			continue
		}
		laid = append(laid, gateCorpus{corpus: each, pins: pins[each.Name], checkout: checkout})
	}
	return laid, unlaid, nil
}

// corporaArgument is the gate's Mac script's corpora argument: each corpus's variable and place, `VARIABLE=place`,
// comma-separated, or "-" for none, so the argument is never empty.
func corporaArgument(corpora []gateCorpus) string {
	pairs := []string{}
	for _, each := range corpora {
		pairs = append(pairs, each.corpus.Variable+"="+each.place())
	}
	if len(pairs) == 0 {
		return "-"
	}
	return strings.Join(pairs, ",")
}

// corporaNamed is how the land line names the corpora a box held, each at its checkout commit.
func corporaNamed(corpora []gateCorpus) string {
	named := []string{}
	for _, each := range corpora {
		named = append(named, each.corpus.Name+" at "+shortCommit(each.pins.Checkout))
	}
	return strings.Join(named, ", ")
}

// unlaidIn is the places a box's run said it lacks, nil when it said nothing of them.
func unlaidIn(output []byte) []string {
	for line := range strings.SplitSeq(string(output), "\n") {
		if places, found := strings.CutPrefix(strings.TrimSpace(line), corpusUnlaidMarker); found {
			return strings.Fields(places)
		}
	}
	return nil
}

// corpusLayScript lays each corpus a carry holds, as loom, under macBoxScratch/corpora. Its arguments are the carry and
// one `name/laid/install/checkout` for each corpus in it, laid being its place's leaf (the commit and its recipe's
// digest) and checkout empty but for a repository's. The carry is a gzip tar holding each corpus's tree at its place.
// Each is installed in a staging directory beside its place, made read-only, and only then renamed into place, so a
// place that exists is whole; a corpus whose place exists is left alone. Every other commit of a corpus it lays is
// removed, so a box holds one of each and its disk stays bounded. The node that installs is the pinned one, checked by
// its digest as sharedBoxGate checks it.
const corpusLayScript = `set -euo pipefail
carry="$1"; shift
base=` + macBoxScratch + `/corpora
refuse() { echo "` + sharedBoxRefusedMarker + `$*"; exit 1; }
node="` + macBoxNode + `"; nodeDigest="` + macBoxNodeDigest + `"
[ -x "$node/bin/node" ] || refuse "there is no node at $node to install the corpora with"
nodeFound=$(sha256sum "$node/bin/node" | cut -d' ' -f1)
[ "$nodeFound" = "$nodeDigest" ] || refuse "the node at $node/bin/node digests to $nodeFound, not the pinned $nodeDigest"
mkdir -p "$base"
free=$(df -k "$base" | awk 'NR == 2 { print int($4 / 1048576) }')
floor=` + corpusFreeFloor + `
[ "$free" -ge "$floor" ] || refuse "only ${free} GiB is free on this box, under the ${floor} GiB the corpora leave"
unpacked=$(mktemp -d "${TMPDIR:-/tmp}/cohere-corpora.XXXXXX")
trap 'chmod -R u+w "$unpacked" 2>/dev/null; rm -rf "$unpacked"' EXIT
tar -x -z -f "$carry" -C "$unpacked"
export PATH="$node/bin:/usr/bin:/bin" COREPACK_ENABLE_DOWNLOAD_PROMPT=0
for spec in "$@"; do
  IFS=/ read -r name commit install checkout <<< "$spec"
  place="$base/$name/$commit"
  if [ ! -e "$place" ]; then
    staging="$base/$name/.staging-$commit"
    if [ -e "$staging" ]; then chmod -R u+w "$staging"; rm -rf "$staging"; fi
    mkdir -p "$base/$name"
    mv "$unpacked/$name/$commit" "$staging"
    started=$(date +%s)
    case "$install" in
      none) ;;
      pnpm) said=$(cd "$staging" && corepack pnpm install --frozen-lockfile --ignore-scripts 2>&1) ||
          refuse "$name's pnpm install at $commit failed: $(echo "$said" | tail -n 1)" ;;
      fork) said=$(cd "$staging" && set -- .yarn/releases/yarn-*.cjs && [ "$#" -eq 1 ] &&
          node "$1" install --immutable 2>&1) || refuse "$name's yarn install at $commit failed: $(echo "$said" | tail -n 1)"
        said=$(cd "$staging" && node scripts/build/build.js` + corpusForkBundleFlags + ` 2>&1) ||
          refuse "$name's bundles at $commit didn't build: $(echo "$said" | tail -n 1)" ;;
      *) refuse "no install is named $install" ;;
    esac
    if [ -n "$checkout" ]; then
      git -C "$staging" config core.bare false
      git -C "$staging" checkout -q -f --detach "$checkout" || refuse "$name holds no $checkout to check out"
    fi
    chmod -R a-w "$staging"
    mv "$staging" "$place"
    echo "` + corpusLaidMarker + `$name at $commit in $(( $(date +%s) - started ))s, $(du -sh "$place" | cut -f1)"
  fi
  for other in "$base/$name"/*; do
    [ "$other" = "$place" ] || [ ! -e "$other" ] || { chmod -R u+w "$other"; rm -rf "$other"; echo "` +
	corpusLaidMarker + `$name at $(basename "$other") removed"; }
  done
done
`

// corpusRecipeDigest is the first 8 hex digits of the sha256 of how a corpus is laid: the lay script and its recipe.
func corpusRecipeDigest(name string) string {
	return recipeDigestOf(corpusLayScript, corpusRecipes[name])
}

// recipeDigestOf is corpusRecipeDigest of a recipe laid by script.
func recipeDigestOf(script string, recipe corpusRecipe) string {
	hash := sha256.Sum256(fmt.Appendf(nil, "%s\x00%q\x00%s\x00%t", script, recipe.excluded, recipe.install,
		recipe.repository))
	return hex.EncodeToString(hash[:])[:8]
}

// layCorpora lays on box the corpora at places, the ones her run said she lacks, in a gap of their own carrying them,
// saying so to lines, and returns what the gap said, or why they weren't laid, which refuses the landing.
func layCorpora(
	worktree string,
	box string,
	corpora []gateCorpus,
	places []string,
	lines io.Writer,
) (string, error) {
	laying := []gateCorpus{}
	for _, each := range corpora {
		if slices.Contains(places, each.place()) {
			laying = append(laying, each)
		}
	}
	if len(laying) != len(places) {
		return "", fmt.Errorf("%s lacks %s, which this landing didn't ask her for", box, strings.Join(places, " "))
	}
	directory, directoryError := os.MkdirTemp("", "cohere-dev-corpora-")
	if directoryError != nil {
		return "", directoryError
	}
	defer func() {
		if removeError := os.RemoveAll(directory); removeError != nil {
			// Ignored: a line to the terminal that never arrives loses nothing the verdict needs, and the
			// stream keeps its error for streams.Finish to report.
			fmt.Fprintf(lines, "cohere-dev: the corpora's scratch %s stays: %v\n", directory, removeError)
		}
	}()
	carry := filepath.Join(directory, "corpora.tar.gz")
	specs, carryError := writeCorpusCarry(carry, directory, laying)
	if carryError != nil {
		return "", carryError
	}
	information, statError := os.Stat(carry)
	if statError != nil {
		return "", statError
	}
	if information.Size() > corpusCarryLimit {
		return "", fmt.Errorf("the corpora to lay come to %d MiB, over the %d MiB a gap carries",
			information.Size()>>20, corpusCarryLimit>>20)
	}
	digest, digestError := fileDigest(carry)
	if digestError != nil {
		return "", digestError
	}
	// Ignored: a line to the terminal that never arrives loses nothing the verdict needs, and the stream keeps its
	// error for streams.Finish to report.
	fmt.Fprintf(lines, "cohere-dev: %s lacks %s, so it is laid there, %d MiB carried, and she is asked again\n", box,
		strings.Join(places, ", "), information.Size()>>20)
	remote := "echo " + base64.StdEncoding.EncodeToString([]byte(corpusLayScript)) + " | base64 -d | bash -s -- " +
		"\"$TMPDIR/carry/" + digest + "\" " + strings.Join(specs, " ")
	command, loomError := loomCommand(worktree, "loom", "gap", "--box", box, "--within", corpusLayBound, "--carry",
		carry, "--carry-sha256", digest, "--", "ssh", box, remote)
	if loomError != nil {
		return "", loomError
	}
	output, runError := command.CombinedOutput()
	verdict := readTestJson(output)
	switch {
	case verdict.refused != "":
		return string(output), fmt.Errorf("%s refused to lay the corpora: %s", box, verdict.refused)
	case runError != nil:
		return string(output), fmt.Errorf("laying the corpora on %s ended %s (%s)", box, describeEnd(runError),
			lastWords(output, describeEnd(runError)))
	}
	return string(output), nil
}

// writeCorpusCarry writes the carry corpusLayScript reads to carry: each corpus's tree, or its repository, at
// `name/commit`, from the lander's checkout, using directory for a repository's scratch. It returns each corpus's spec
// for the script.
func writeCorpusCarry(carry string, directory string, corpora []gateCorpus) ([]string, error) {
	file, createError := os.Create(carry)
	if createError != nil {
		return nil, createError
	}
	// Ignored: the write path closes the file itself and returns that error; this close only frees it on a failure.
	defer file.Close()
	compressed := gzip.NewWriter(file)
	archive := tar.NewWriter(compressed)
	specs := []string{}
	for _, each := range corpora {
		recipe, known := corpusRecipes[each.corpus.Name]
		if !known {
			return nil, fmt.Errorf("no recipe lays %s", each.corpus.Name)
		}
		prefix := each.place() + "/"
		checkout := ""
		if recipe.repository {
			if repositoryError := addCorpusRepository(archive, prefix, directory, each); repositoryError != nil {
				return nil, repositoryError
			}
			checkout = each.pins.Checkout
		} else if treeError := addCorpusTree(archive, prefix, each, recipe.excluded); treeError != nil {
			return nil, treeError
		}
		specs = append(specs, each.place()+"/"+recipe.install+"/"+checkout)
	}
	if closeError := archive.Close(); closeError != nil {
		return nil, closeError
	}
	if closeError := compressed.Close(); closeError != nil {
		return nil, closeError
	}
	return specs, file.Close()
}

// addCorpusTree adds the corpus's tree at its checkout pin under prefix, leaving out excluded, with each submodule the
// tree records filled in at its commit from the checkout's own repository of it.
func addCorpusTree(archive *tar.Writer, prefix string, each gateCorpus, excluded []string) error {
	pathspecs := []string{"."}
	for _, path := range excluded {
		pathspecs = append(pathspecs, ":(exclude)"+path)
	}
	if copyError := copyGitArchive(archive, each.checkout, each.pins.Checkout, prefix, pathspecs); copyError != nil {
		return fmt.Errorf("%s at %s: %w", each.corpus.Name, shortCommit(each.pins.Checkout), copyError)
	}
	links, listError := gitOutput(each.checkout, "ls-tree", "-r", "-z", each.pins.Checkout)
	if listError != nil {
		return listError
	}
	for entry := range strings.SplitSeq(links, "\x00") {
		fields, path, found := strings.Cut(entry, "\t")
		parts := strings.Fields(fields)
		isLeftOut := func(left string) bool { return path == left || strings.HasPrefix(path, left+"/") }
		if !found || len(parts) != 3 || parts[0] != "160000" || slices.ContainsFunc(excluded, isLeftOut) {
			continue
		}
		submodule := filepath.Join(each.checkout, filepath.FromSlash(path))
		if copyError := copyGitArchive(archive, submodule, parts[2], prefix+path+"/", []string{"."}); copyError != nil {
			return fmt.Errorf("%s's submodule %s at %s, from %s: %w", each.corpus.Name, path, shortCommit(parts[2]),
				submodule, copyError)
		}
	}
	return nil
}

// copyGitArchive copies `git archive` of commit in repository, limited to pathspecs, into archive under prefix.
func copyGitArchive(archive *tar.Writer, repository string, commit string, prefix string, pathspecs []string) error {
	command := exec.Command("git", append([]string{"-C", repository, "archive", "--format=tar", commit, "--"},
		pathspecs...)...)
	var standardError strings.Builder
	command.Stderr = &standardError
	pipe, pipeError := command.StdoutPipe()
	if pipeError != nil {
		return pipeError
	}
	if startError := command.Start(); startError != nil {
		return startError
	}
	reader := tar.NewReader(pipe)
	var copyError error
	for {
		header, nextError := reader.Next()
		if errors.Is(nextError, io.EOF) {
			break
		}
		if nextError != nil {
			copyError = nextError
			break
		}
		// git archive opens with a global header naming the commit, which describes no file.
		if header.Typeflag == tar.TypeXGlobalHeader {
			continue
		}
		header.Name = prefix + header.Name
		// The writer picks the format, since the prefix can push a name past what git's choice of `USTAR` holds: ahra
		// has paths that fit it alone and not under a place (Sun2, Nov 16).
		header.Format = tar.FormatUnknown
		if writeError := archive.WriteHeader(header); writeError != nil {
			copyError = writeError
			break
		}
		if _, writeError := io.Copy(archive, reader); writeError != nil {
			copyError = writeError
			break
		}
	}
	// Ignored: draining only keeps git from blocking on a pipe nothing reads after a copy failed, and git's own
	// exit, read next, says whether the archive was whole.
	_, _ = io.Copy(io.Discard, pipe)
	if waitError := command.Wait(); waitError != nil {
		return fmt.Errorf("git archive %s: %w: %s", shortCommit(commit), waitError,
			strings.TrimSpace(standardError.String()))
	}
	return copyError
}

// addCorpusRepository adds, under prefix, a repository holding every commit of the corpus a test reads through At, each
// on a ref of its own so nothing prunes it, fetched from the lander's checkout into a bare repository in directory.
func addCorpusRepository(archive *tar.Writer, prefix string, directory string, each gateCorpus) error {
	bare := filepath.Join(directory, each.corpus.Name+".git")
	if _, initError := gitOutput(directory, "init", "-q", "--bare", bare); initError != nil {
		return initError
	}
	references := []string{"fetch", "-q", "--no-tags", each.checkout}
	for _, commit := range each.pins.Held {
		references = append(references, commit+":refs/pins/"+commit)
	}
	if _, fetchError := gitOutput(bare, references...); fetchError != nil {
		return fmt.Errorf("%s: %w", each.corpus.Name, fetchError)
	}
	return filepath.WalkDir(bare, func(path string, entry fs.DirEntry, walkError error) error {
		if walkError != nil {
			return walkError
		}
		relative, relativeError := filepath.Rel(bare, path)
		if relativeError != nil {
			return relativeError
		}
		information, informationError := entry.Info()
		if informationError != nil {
			return informationError
		}
		header, headerError := tar.FileInfoHeader(information, "")
		if headerError != nil {
			return headerError
		}
		header.Name = prefix + ".git/" + filepath.ToSlash(relative)
		if entry.IsDir() {
			header.Name += "/"
		}
		if writeError := archive.WriteHeader(header); writeError != nil {
			return writeError
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		contents, openError := os.Open(path)
		if openError != nil {
			return openError
		}
		// Ignored: the file is only read, so its close has nothing to lose.
		defer contents.Close()
		_, copyError := io.Copy(archive, contents)
		return copyError
	})
}

// corpusLaidLines is the lines of a lay gap's output that name what it laid or removed, each as cohere-dev says it.
func corpusLaidLines(output string) string {
	lines := ""
	for line := range strings.SplitSeq(output, "\n") {
		if laid, found := strings.CutPrefix(strings.TrimSpace(line), corpusLaidMarker); found {
			lines += "cohere-dev: laid " + laid + "\n"
		}
	}
	return lines
}

// fileDigest is a file's sha256, in hex.
func fileDigest(path string) (string, error) {
	file, openError := os.Open(path)
	if openError != nil {
		return "", openError
	}
	// Ignored: the file is only read, so its close has nothing to lose.
	defer file.Close()
	hash := sha256.New()
	if _, copyError := io.Copy(hash, file); copyError != nil {
		return "", copyError
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
