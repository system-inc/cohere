package high_level_intermediate_representation

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/core"
	"github.com/microsoft/TypeScript/tsc/shim/parser"
	"github.com/microsoft/TypeScript/tsc/shim/tspath"
	"github.com/system-inc/cohere/internal/corpus"
)

// corpusRoot is a real TypeScript codebase to lower against.
//
// # Why a path outside the repository, and why the test skips rather than fails
//
// Toy inputs agree with whatever the author imagined. Every construct in the hand-written tests was
// written by the same person who wrote the lowering for it, so both can share a misconception and
// the test still passes. Real code was written by someone with no knowledge of this IR, which is
// the only kind of input that can disagree with it.
//
// It is a checkout rather than vendored files because vendoring would freeze the corpus, and the value
// here is precisely that the code keeps changing under the lowering. Structure is private, so the
// checkout is the structure corpus (corpus.Structure, COHERE_CORPUS_STRUCTURE), and the test skips where
// it is not set, naming the variable, and the gate counts it as not covered (#sycrdr6). A skipping test proves nothing
// on CI, and that is accepted deliberately: this test is a development instrument for finding the
// constructs a hand-written suite forgot, and every defect it finds gets a hand-written test of its
// own. Two such defects were found on the first run and both are pinned in lower_test.go.
// The whole library rather than one subdirectory. The first version pointed at `utilities/` and
// lowered 171 functions cleanly, which read as a strong result until the coverage was checked
// against the constructs the design was most likely to get wrong. That subtree happens to contain
// no `switch` written with a space after the keyword, and the grep used to confirm coverage was
// itself wrong, so "no unsupported constructs" was measuring a corpus far narrower than intended.
// Widening it is the cheap fix and it costs about a second.
//
// It is the library at corpus.StructurePin, so a landing never reads what was pushed that hour. The nightly
// reads the live checkout through corpus.LiveVariable, where new code, and so the unanticipated construct, is
// (#jf5dz34).
func corpusRoot(test testing.TB) string {
	test.Helper()
	return corpus.Structure.At(test, corpus.StructurePin, "source")
}

// pinnedCorpusCommit is a commit in the structure corpus's repository whose files can never change.
//
// # Why the exact counts read a pin of their own
//
// The well-formedness walks read corpus.StructurePin in the gate and the live directory in the nightly:
// they look for a construct nobody anticipated, and new code is where those arrive, so the nightly is where
// that hunt runs, and the gate never reads a tree that moves under a landing (#jf5dz34). A pin that moves
// forward is wrong for a test that asserts exact counts, because the counts then move whenever anyone edits
// the directory, and our own lint sweeps edit it most. `TestHoistableCorpusDistribution` asserts exact
// counts on purpose (its comment records the mutants only an exact count can see), and against the live
// tree it failed after every sweep in both directions, 796 to 800 and then to 773. So it reads the same
// paths at this commit instead, and its numbers move only when the analysis does.
const (
	pinnedCorpusCommit = "9f40ee3e313a9c78b8e98da4cd214d45af4b7495"
)

// fastTierVariable is set by `cohere-dev test --fast`, the edit loop, which leaves the corpus walks to the
// landing gate (#nxgt2ca). A test that walks the corpus reaches it through skipWithoutCorpus or
// pinnedCorpusFiles, so a new one is left out of the fast tier without anyone listing it. Go's test cache
// keys on the variable, since a test reads it, so a fast run's result never answers for a full one.
const fastTierVariable = "COHERE_FAST_TIER"

// skipCorpusWalkInFastTier skips a corpus walk under the fast tier, saying so.
func skipCorpusWalkInFastTier(test *testing.T) {
	test.Helper()
	if os.Getenv(fastTierVariable) != "" {
		test.Skip("a corpus walk, left to the landing gate by cohere-dev test --fast")
	}
}

// skipWithoutCorpus skips a test that walks the live corpus when the fast tier runs it, or when the
// structure corpus is not set.
func skipWithoutCorpus(test *testing.T) {
	test.Helper()
	skipCorpusWalkInFastTier(test)
	corpus.Structure.Root(test)
}

// pinnedCorpus is each count's read of the pinned corpus, made once per test binary. About thirty corpus
// walks ask for it, and each used to list the commit and start a `git show` per file, about 10,000 git
// processes in one package run (#nxgt2ca). The commit is pinned, so the files cannot change between
// walks, and every caller only reads what it is handed.
var pinnedCorpus = struct {
	sync.Mutex
	byCount map[int]*pinnedCorpusRead
}{byCount: map[int]*pinnedCorpusRead{}}

type pinnedCorpusRead struct {
	once     sync.Once
	paths    []string
	contents map[string]string

	// failure, when set, is what every caller reports instead: a test cannot fail another test, so the
	// read records the outcome and each caller acts on it. A corpus that is not set skips each caller
	// before the read, through corpus.Structure.
	failure string
}

// pinnedCorpusFiles returns the first count TypeScript paths under `source/` at pinnedCorpusCommit,
// sorted, with each one's contents at that commit.
//
// It skips when the structure corpus is not set, as the live tests do. It fails when the corpus is set
// and the commit or a file is not in it, and when fewer than count files come back,
// because each of those would otherwise measure a different corpus and still report a number.
func pinnedCorpusFiles(test *testing.T, count int) ([]string, map[string]string) {
	test.Helper()
	skipCorpusWalkInFastTier(test)

	pinnedCorpus.Lock()
	read := pinnedCorpus.byCount[count]
	if read == nil {
		read = &pinnedCorpusRead{}
		pinnedCorpus.byCount[count] = read
	}
	pinnedCorpus.Unlock()

	repository := corpus.Structure.Root(test)
	read.once.Do(func() { read.paths, read.contents, read.failure = readPinnedCorpus(repository, count) })
	if read.failure != "" {
		test.Fatal(read.failure)
	}
	return read.paths, read.contents
}

// readPinnedCorpus lists the first count TypeScript paths at the pinned commit and reads them through
// one `git cat-file --batch`.
func readPinnedCorpus(repository string, count int) (paths []string, contents map[string]string, failure string) {
	listing, outputError := exec.Command("git", "-C", repository, "ls-tree", "-r", "--name-only",
		pinnedCorpusCommit, "--", "source").Output()
	if outputError != nil {
		return nil, nil, fmt.Sprintf("listing %s at %s: %v; the repository is here and the pinned commit is not",
			repository, pinnedCorpusCommit, outputError)
	}
	for _, line := range strings.Split(string(listing), "\n") {
		if strings.HasSuffix(line, ".ts") || strings.HasSuffix(line, ".tsx") {
			paths = append(paths, line)
		}
	}
	sort.Strings(paths)
	if len(paths) < count {
		return nil, nil, fmt.Sprintf("the pinned corpus holds %d TypeScript files, want at least %d", len(paths), count)
	}
	paths = paths[:count]

	var request strings.Builder
	for _, path := range paths {
		request.WriteString(pinnedCorpusCommit + ":" + path + "\n")
	}
	batch := exec.Command("git", "-C", repository, "cat-file", "--batch")
	batch.Stdin = strings.NewReader(request.String())
	output, outputError := batch.Output()
	if outputError != nil {
		return nil, nil, fmt.Sprintf("reading the corpus at %s: %v", pinnedCorpusCommit, outputError)
	}
	// Each answer is "<object> blob <size>\n", the size in bytes of contents, then "\n".
	contents = make(map[string]string, len(paths))
	reader := bufio.NewReader(bytes.NewReader(output))
	for _, path := range paths {
		header, readStringError := reader.ReadString('\n')
		fields := strings.Fields(header)
		if readStringError != nil || len(fields) != 3 || fields[1] != "blob" {
			return nil, nil, fmt.Sprintf(
				"reading %s at %s: git answered %q",
				path,
				pinnedCorpusCommit,
				strings.TrimSpace(header),
			)
		}
		size, parseError := strconv.Atoi(fields[2])
		if parseError != nil {
			return nil, nil, fmt.Sprintf("reading %s at %s: a size of %q", path, pinnedCorpusCommit, fields[2])
		}
		blob := make([]byte, size+1)
		if _, readFullError := io.ReadFull(reader, blob); readFullError != nil || blob[size] != '\n' {
			return nil, nil, fmt.Sprintf("reading %s at %s: the blob ended early", path, pinnedCorpusCommit)
		}
		contents[path] = string(blob[:size])
	}
	return paths, contents, ""
}

// TestLowerRealCodebase lowers every function in a real TypeScript tree and checks the invariants.
//
// It asserts nothing about what the IR says, only that what it produces is well-formed: terminals
// set, successors existing, predecessors agreeing in both directions, evaluation order assigned and
// increasing, every place naming a real identifier. Those are the properties every pass built on
// this will assume, and they are the ones a construct nobody anticipated will break.
func TestLowerRealCodebase(test *testing.T) {
	test.Parallel()

	skipWithoutCorpus(test)

	var files []string
	walkError := filepath.Walk(corpusRoot(test), func(path string, info os.FileInfo, pathError error) error {
		if pathError != nil {
			return nil
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".ts") && !strings.HasSuffix(path, ".tsx") {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if walkError != nil {
		test.Fatalf("walking the corpus: %v", walkError)
	}
	if len(files) < 10 {
		test.Fatalf("the corpus holds only %d files; the path is probably wrong", len(files))
	}

	totalFunctions := 0
	totalBlocks := 0
	totalInstructions := 0
	unsupported := map[string]int{}

	for _, path := range files {
		contents, readError := os.ReadFile(path)
		if readError != nil {
			continue
		}
		kind := core.ScriptKindTS
		if strings.HasSuffix(path, ".tsx") {
			kind = core.ScriptKindTSX
		}
		source := parser.ParseSourceFile(ast.SourceFileParseOptions{
			FileName: tspath.RootedFilePath(path),
			PathKey:  tspath.PathKey("/" + filepath.Base(path)),
		}, string(contents), kind)

		forEachFunctionLike(source.AsNode(), func(node *ast.Node) {
			function := Lower(node, nil)
			if function == nil {
				return
			}
			totalFunctions++

			// Run the invariants against a subtest so a failure names the file and function.
			name := function.Name
			if name == "" {
				name = "<anonymous>"
			}
			test.Run(filepath.Base(path)+"/"+name, func(test *testing.T) {
				test.Parallel()
				checkInvariants(test, function)
			})

			var count func(*Function)
			count = func(function *Function) {
				totalBlocks += len(function.Blocks)
				totalInstructions += len(function.Instructions)
				for _, instruction := range function.Instructions {
					if node, ok := instruction.Value.(*UnsupportedNode); ok {
						unsupported[node.Reason]++
					}
				}
				for _, nested := range function.Functions {
					count(nested)
				}
			}
			count(function)
		})
	}

	if totalFunctions < 1000 {
		test.Errorf("lowered only %d functions from %d files; the walk is probably not finding them",
			totalFunctions, len(files))
	}

	test.Logf("lowered %d functions from %d files: %d blocks, %d instructions",
		totalFunctions, len(files), totalBlocks, totalInstructions)

	// The unsupported tally is reported rather than asserted. It is the measurement that tells the
	// next person what completing this lowering actually costs, and asserting a number would make
	// the test fail when the corpus changes rather than when the lowering does.
	if len(unsupported) > 0 {
		var lines []string
		for reason, count := range unsupported {
			lines = append(lines, reason+"="+itoa(count))
		}
		test.Logf("unsupported constructs encountered: %s", strings.Join(lines, " "))
	}
}

// TestLowerRealCodebaseIsDeterministic pins that lowering the same input twice gives the same
// answer.
//
// Nondeterminism here would be invisible in every other test and fatal to a golden-file suite
// downstream. The usual source is map iteration order leaking into block or identifier numbering,
// which this package avoids by allocating ids from counters rather than from map walks.
//
// The sample is read from the pinned corpus, whose files never change. It used to be a named file in the
// live tree, style/ColorConverter.ts, which moved away and left the test skipping on every machine (#sycrdr6).
// It is the first pinned file that lowers to something, so the comparison is never over nothing.
func TestLowerRealCodebaseIsDeterministic(test *testing.T) {
	test.Parallel()

	paths, contents := pinnedCorpusFiles(test, 100)

	printOnce := func(path string) string {
		kind := core.ScriptKindTS
		if strings.HasSuffix(path, ".tsx") {
			kind = core.ScriptKindTSX
		}
		source := parser.ParseSourceFile(ast.SourceFileParseOptions{
			FileName: tspath.RootedFilePath("/" + path),
			PathKey:  tspath.PathKey("/" + path),
		}, contents[path], kind)

		var out strings.Builder
		forEachFunctionLike(source.AsNode(), func(node *ast.Node) {
			if function := Lower(node, nil); function != nil {
				out.WriteString(Print(function))
			}
		})
		return out.String()
	}

	sample := ""
	for _, path := range paths {
		if printOnce(path) != "" {
			sample = path
			break
		}
	}
	if sample == "" {
		test.Fatalf("none of the %d pinned files lowers to anything, so there is no sample to compare", len(paths))
	}
	first := printOnce(sample)
	second := printOnce(sample)
	if first != second {
		test.Error("lowering the same file twice produced different output; something depends on map order")
	}
	test.Logf("lowered %s twice, %d bytes each", sample, len(first))
}

// forEachFunctionLike calls visit for every function-like node in the tree, outermost first.
//
// Nested functions are visited by `Lower` through the function arena, so this deliberately does not
// descend into a function it has already handed over: doing so would lower each inner function
// twice, once standalone and once as a child, and the standalone copy would have no context.
func forEachFunctionLike(root *ast.Node, visit func(*ast.Node)) {
	if root == nil {
		return
	}
	root.ForEachChild(func(node *ast.Node) bool {
		if ast.IsFunctionLike(node) {
			visit(node)
			return false
		}
		forEachFunctionLike(node, visit)
		return false
	})
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	digits := ""
	for remaining := value; remaining > 0; remaining /= 10 {
		digits = string(rune('0'+remaining%10)) + digits
	}
	return digits
}
