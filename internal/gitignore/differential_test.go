package gitignore

import (
	"bytes"
	"fmt"
	"io/fs"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// The differential: on a real tree, every path's verdict and deciding rule equals git check-ignore's, read
// with the index as git reads it, so a path the index tracks, or a directory holding one, is not ignored.
//
// git runs here and nowhere else, as the oracle. It reads the trees named by COHERE_GITIGNORE_TREES
// (colon separated), and every repository nested in them is compared as a repository of its own, the way
// a walk meets it. git runs under sandbox-exec, with no network and no writes outside a temporary
// directory, and the sandbox is shown to hold before git reads anything.
func TestTheMatcherAgreesWithGitOnRealTrees(test *testing.T) {
	test.Parallel()
	trees := os.Getenv("COHERE_GITIGNORE_TREES")
	if trees == "" {
		test.Skip("COHERE_GITIGNORE_TREES names no trees to compare")
	}
	oracle := newOracle(test)
	for _, tree := range strings.Split(trees, ":") {
		pending := []string{tree}
		for len(pending) > 0 {
			root := pending[0]
			pending = pending[1:]
			comparison := compareWithGit(test, oracle, root)
			pending = append(pending, comparison.nested...)
			test.Logf(
				"%s: %d paths compared (%d ignored, %d re-included by a negation, %d decided by no rule, %d tracked though a rule matches), %d ignore files read, %d nested repositories, %d differences",
				root,
				comparison.compared,
				comparison.ignored,
				comparison.reincluded,
				comparison.undecided,
				comparison.trackedUnderRule,
				comparison.ignoreFiles,
				len(comparison.nested),
				len(comparison.differences),
			)
			for index, difference := range comparison.differences {
				if index == 20 {
					test.Errorf("%s: %d more differences", root, len(comparison.differences)-20)
					break
				}
				test.Errorf("%s: %s", root, difference)
			}
			if comparison.compared == 0 {
				test.Errorf("%s: no path was compared, so agreeing proves nothing", root)
			}
		}
	}
}

// The same differential over generated trees: random names, random ignore files at random depths, and
// patterns drawn from every construct gitignore(5) describes, so the matcher meets combinations no real
// tree holds. Some files of each tree are added to its index whatever the patterns say, as `git add -f`
// adds them, so a tracked path under a matching pattern, and inside an excluded directory, is compared too.
// COHERE_GITIGNORE_GENERATED is how many trees, COHERE_GITIGNORE_SEED fixes the sequence.
func TestTheMatcherAgreesWithGitOnGeneratedTrees(test *testing.T) {
	test.Parallel()
	generated := os.Getenv("COHERE_GITIGNORE_GENERATED")
	count, countError := strconv.Atoi(generated)
	if countError != nil && generated != "" {
		test.Fatalf("COHERE_GITIGNORE_GENERATED is %q, which is not a count of trees: %v", generated, countError)
	}
	if count <= 0 {
		test.Skip("COHERE_GITIGNORE_GENERATED asks for no generated trees")
	}
	seed, parseError := strconv.ParseInt(os.Getenv("COHERE_GITIGNORE_SEED"), 10, 64)
	if parseError != nil {
		seed = rand.Int63()
	}
	test.Logf("seed %d", seed)
	random := rand.New(rand.NewSource(seed))
	oracle := newOracle(test)

	compared, ignored, reincluded, trackedUnderRule, failing := 0, 0, 0, 0, 0
	for tree := 0; tree < count; tree++ {
		root := filepath.Join(oracle.scratch, fmt.Sprintf("generated-%d", tree))
		exclude := generateTree(test, random, root)
		oracle.run(test, root, nil, "init", "--quiet")
		if exclude != "" {
			writeTree(test, root, map[string]string{ExcludeFile: exclude})
		}
		trackRandomFiles(test, oracle, random, root)
		comparison := compareWithGit(test, oracle, root)
		compared += comparison.compared
		ignored += comparison.ignored
		reincluded += comparison.reincluded
		trackedUnderRule += comparison.trackedUnderRule
		if len(comparison.differences) > 0 {
			failing++
			if failing <= 5 {
				test.Errorf(
					"generated tree %d (%s), %d differences, first: %s",
					tree,
					root,
					len(comparison.differences),
					comparison.differences[0],
				)
			}
		}
	}
	test.Logf(
		"%d generated trees, %d paths compared (%d ignored, %d re-included by a negation, %d tracked though a rule matches), %d trees with a difference",
		count,
		compared,
		ignored,
		reincluded,
		trackedUnderRule,
		failing,
	)
	// Trees whose rules decided nothing, or whose index tracked nothing a rule matched, would agree with git
	// without testing the matcher.
	if compared == 0 || ignored == 0 || reincluded == 0 || trackedUnderRule == 0 {
		test.Errorf(
			"the generated trees exercised too little: %d compared, %d ignored, %d re-included, %d tracked though a rule matches",
			compared,
			ignored,
			reincluded,
			trackedUnderRule,
		)
	}
}

// oracle runs git sandboxed.
type oracle struct {
	scratch      string
	profile      string
	emptyExclude string
}

func newOracle(test *testing.T) *oracle {
	test.Helper()
	if runtime.GOOS != "darwin" {
		test.Skip("the oracle sandbox is sandbox-exec, which is macOS only")
	}
	scratch, resolveSymlinksError := filepath.EvalSymlinks(test.TempDir())
	if resolveSymlinksError != nil {
		test.Fatal(resolveSymlinksError)
	}
	profile := filepath.Join(scratch, "oracle.sb")
	rules := fmt.Sprintf(`(version 1)
(allow default)
(deny network*)
(deny file-write*)
(allow file-write* (subpath %q) (literal "/dev/null") (regex #"^/dev/fd/") (literal "/dev/stdout") (literal "/dev/stderr"))
`, scratch)
	if writeError := os.WriteFile(profile, []byte(rules), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	emptyExclude := filepath.Join(scratch, "empty-excludes")
	if writeError := os.WriteFile(emptyExclude, nil, 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	result := &oracle{scratch: scratch, profile: profile, emptyExclude: emptyExclude}

	// The sandbox must hold before git reads a real tree: a write outside the scratch directory and a
	// network connection both fail, and a write inside succeeds.
	outside := filepath.Join(os.Getenv("HOME"), fmt.Sprintf(".cohere-sandbox-probe-%d", os.Getpid()))
	if runError := exec.Command("sandbox-exec", "-f", profile, "/usr/bin/touch", outside).Run(); runError == nil {
		// Ignored: the test fails on the next line whatever the remove does, and the probe is named for its pid.
		_ = os.Remove(outside)
		test.Fatal("the sandbox let a write outside its scratch directory through")
	}
	if runError := exec.Command(
		"sandbox-exec",
		"-f",
		profile,
		"/usr/bin/touch",
		filepath.Join(scratch, "inside"),
	).Run(); runError != nil {
		test.Fatalf("the sandbox refused a write inside its scratch directory: %v", runError)
	}
	if runError := exec.Command(
		"sandbox-exec",
		"-f",
		profile,
		"/usr/bin/nc",
		"-z",
		"-G",
		"2",
		"1.1.1.1",
		"443",
	).Run(); runError == nil {
		test.Fatal("the sandbox let a network connection through")
	}
	return result
}

// run runs git in repository, sandboxed, with no configuration but the repository's own, no global
// excludes file, and case sensitive matching.
func (oracle *oracle) run(test *testing.T, repository string, stdin []byte, arguments ...string) []byte {
	test.Helper()
	command := exec.Command("sandbox-exec", append([]string{"-f", oracle.profile, "git",
		"-c", "core.excludesFile=" + oracle.emptyExclude, "-c", "core.ignorecase=false"}, arguments...)...)
	command.Dir = repository
	command.Env = []string{
		"PATH=/usr/bin:/bin", "HOME=" + oracle.scratch, "XDG_CONFIG_HOME=" + oracle.scratch,
		"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_OPTIONAL_LOCKS=0",
	}
	command.Stdin = bytes.NewReader(stdin)
	var standardError bytes.Buffer
	command.Stderr = &standardError
	output, outputError := command.Output()
	// check-ignore exits 1 when nothing is ignored, which is an answer, not a failure.
	if exitError, ok := outputError.(*exec.ExitError); ok &&
		exitError.ExitCode() == 1 &&
		arguments[0] == "check-ignore" {
		outputError = nil
	}
	if outputError != nil {
		test.Fatalf(
			"git %s in %s: %v\n%s",
			strings.Join(arguments, " "),
			repository,
			outputError,
			standardError.String(),
		)
	}
	return output
}

// verdict is one path's answer.
type verdict struct {
	ignored bool
	source  Source
}

type comparison struct {
	compared, ignored, reincluded, undecided, ignoreFiles int
	// trackedUnderRule counts the paths the index tracks that the ignore files alone would ignore: the answer
	// reading the index changes.
	trackedUnderRule int
	nested           []string
	differences      []string
}

// compareWithGit decides every path in the repository at root with the matcher and with git, and lists
// where they differ. Nested repositories are not entered; they are returned for their own comparison.
func compareWithGit(test *testing.T, oracle *oracle, root string) comparison {
	test.Helper()
	var result comparison
	matcher, newGitignoreError := New(root)
	if newGitignoreError != nil {
		test.Fatalf("%s: %v", root, newGitignoreError)
	}

	ours := map[string]verdict{}
	var paths []string
	var walk func(scope *Matcher, directory string)
	walk = func(scope *Matcher, directory string) {
		entries, readDirectoryError := os.ReadDir(filepath.Join(root, filepath.FromSlash(directory)))
		if readDirectoryError != nil {
			test.Fatalf("%s: %v", root, readDirectoryError)
		}
		for _, entry := range entries {
			name := entry.Name()
			relative := joinRelative(directory, name)
			if name == ".git" {
				continue
			}
			if name == IgnoreFileName {
				result.ignoreFiles++
			}
			isDirectory := entry.Type()&fs.ModeType == fs.ModeDir
			if isDirectory {
				if _, statError := os.Lstat(
					filepath.Join(root, filepath.FromSlash(relative), ".git"),
				); statError == nil {
					result.nested = append(result.nested, filepath.Join(root, filepath.FromSlash(relative)))
					continue
				}
			}
			ignored, source := scope.Ignored(relative, isDirectory)
			ours[relative] = verdict{ignored, source}
			paths = append(paths, relative)
			rulesOnly := *scope
			rulesOnly.index = nil
			if ignoredByRules, _ := rulesOnly.Ignored(relative, isDirectory); ignoredByRules && !ignored {
				result.trackedUnderRule++
			}
			if isDirectory {
				// Below an excluded directory every untracked path takes the same answer, from the rule that
				// excluded the directory. Its own entries are compared, which shows the answer is inherited;
				// going deeper (node_modules, a build cache) would compare that one fact many thousands of
				// times. A directory holding a tracked path is entered, since its answers differ.
				if excluded, _ := scope.Excluded(); excluded && ignored {
					continue
				}
				child, enterError := scope.Enter(relative)
				if enterError != nil {
					test.Fatalf("%s: %v", root, enterError)
				}
				walk(child, relative)
			}
		}
	}
	walk(matcher, "")

	// check-ignore asks the index whether each path, as a pathspec, matches a tracked path, and a pathspec is a
	// glob there, which `--literal-pathspecs` would fix and check-ignore refuses: `star*/x` would match a
	// tracked `star*/deeper/x`. So a path holding a glob character is asked of the rules alone, with
	// `--no-index`, and of the index by git's own listing of it, read literally: tracked, or a directory holding
	// a tracked path, it is not ignored and no rule decided it, which is what check-ignore answers for one.
	var literal []string
	var globbed []string
	for _, relative := range paths {
		if strings.ContainsAny(relative, `*?[\`) {
			globbed = append(globbed, relative)
		} else {
			literal = append(literal, relative)
		}
	}
	theirs := checkIgnore(test, oracle, root, literal)
	if len(globbed) > 0 {
		listing := oracle.run(test, root, nil, "ls-files", "-z")
		tracked := strings.Split(strings.TrimSuffix(string(listing), "\x00"), "\x00")
		for relative, answer := range checkIgnore(test, oracle, root, globbed, "--no-index") {
			if slices.ContainsFunc(tracked, func(trackedPath string) bool {
				return trackedPath == relative || strings.HasPrefix(trackedPath, relative+"/")
			}) {
				answer = verdict{}
			}
			theirs[relative] = answer
		}
	}

	sort.Strings(paths)
	for _, relative := range paths {
		mine := ours[relative]
		gits, answered := theirs[relative]
		if !answered {
			result.differences = append(result.differences, fmt.Sprintf("%s: git gave no answer", relative))
			continue
		}
		result.compared++
		switch {
		case mine.ignored:
			result.ignored++
		case !mine.source.IsZero():
			result.reincluded++
		default:
			result.undecided++
		}
		if mine != gits {
			result.differences = append(result.differences, fmt.Sprintf("%s: ours %v by %q, git %v by %q",
				relative, mine.ignored, mine.source, gits.ignored, gits.source))
		}
	}
	if len(theirs) != len(paths) {
		result.differences = append(
			result.differences,
			fmt.Sprintf("git answered for %d paths, %d were asked", len(theirs), len(paths)),
		)
	}
	return result
}

// trackRandomFiles adds about a quarter of a generated tree's files to its index, as `git add -f` would, ignored
// or not. update-index reads each path literally, where `git add` would read `star*` as a pattern.
func trackRandomFiles(test *testing.T, oracle *oracle, random *rand.Rand, root string) {
	test.Helper()
	var paths bytes.Buffer
	walkError := filepath.WalkDir(root, func(path string, entry fs.DirEntry, pathError error) error {
		if pathError != nil {
			return pathError
		}
		if entry.IsDir() && entry.Name() == ".git" {
			return filepath.SkipDir
		}
		if !entry.Type().IsRegular() || random.Intn(4) != 0 {
			return nil
		}
		relative, relativeError := filepath.Rel(root, path)
		if relativeError != nil {
			return relativeError
		}
		paths.WriteString(filepath.ToSlash(relative))
		paths.WriteByte(0)
		return nil
	})
	if walkError != nil {
		test.Fatalf("%s: %v", root, walkError)
	}
	if paths.Len() > 0 {
		oracle.run(test, root, paths.Bytes(), "update-index", "--add", "-z", "--stdin")
	}
}

// checkIgnore is git check-ignore's verbose answer for each path, read with the index unless arguments say
// `--no-index`.
func checkIgnore(test *testing.T, oracle *oracle, root string, paths []string, arguments ...string) map[string]verdict {
	test.Helper()
	var input bytes.Buffer
	for _, relative := range paths {
		input.WriteString(relative)
		input.WriteByte(0)
	}
	output := oracle.run(
		test,
		root,
		input.Bytes(),
		append([]string{"check-ignore", "--verbose", "--non-matching", "-z", "--stdin"}, arguments...)...,
	)
	fields := bytes.Split(bytes.TrimSuffix(output, []byte{0}), []byte{0})
	if len(output) > 0 && len(fields)%4 != 0 {
		test.Fatalf("%s: git printed %d fields, not a multiple of four", root, len(fields))
	}
	answers := map[string]verdict{}
	for index := 0; index+3 < len(fields); index += 4 {
		file, line, pattern, relative := string(
			fields[index],
		), string(fields[index+1]), string(fields[index+2]), string(fields[index+3])
		if file == "" {
			answers[relative] = verdict{}
			continue
		}
		lineNumber, parseError := strconv.Atoi(line)
		if parseError != nil {
			test.Fatalf("%s: git printed line %q for %s", root, line, relative)
		}
		answers[relative] = verdict{
			ignored: !strings.HasPrefix(pattern, "!"),
			source:  Source{File: file, Line: lineNumber, Pattern: pattern},
		}
	}
	return answers
}

// generateTree writes a random tree with random ignore files under root, and returns patterns for its
// info/exclude, written once git has made the repository, or "" for none.
func generateTree(test *testing.T, random *rand.Rand, root string) string {
	test.Helper()
	names := []string{"a", "b", "ab", "abc", "x.log", "y.tmp", "build", "out", "node_modules", "keep",
		"#hash", "!bang", "sp ace", "trail ", "star*", "q?", "br[ack]et", "back\\slash", "deep", "Dot.go", ".hidden"}
	var directories = []string{""}
	files := map[string]string{}
	for count := 0; count < 12+random.Intn(20); count++ {
		parent := directories[random.Intn(len(directories))]
		name := names[random.Intn(len(names))]
		if random.Intn(3) == 0 && strings.Count(parent, "/") < 4 {
			directory := joinRelative(parent, name)
			files[directory+"/"] = ""
			directories = append(directories, directory)
			continue
		}
		files[joinRelative(parent, name)] = ""
	}
	for _, directory := range directories {
		if random.Intn(2) == 0 {
			files[joinRelative(directory, IgnoreFileName)] = generatePatterns(random, names)
		}
	}
	writeTreeTolerant(test, root, files)
	if random.Intn(2) == 0 {
		return generatePatterns(random, names)
	}
	return ""
}

// writeTreeTolerant writes a generated tree, skipping a file whose name a directory already took.
func writeTreeTolerant(test *testing.T, root string, files map[string]string) {
	test.Helper()
	keys := make([]string, 0, len(files))
	for name := range files {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	for _, name := range keys {
		full := filepath.Join(root, filepath.FromSlash(name))
		if strings.HasSuffix(name, "/") {
			// Ignored: a file that already took this name keeps it, the tolerance this writer exists for.
			_ = os.MkdirAll(full, 0o755)
			continue
		}
		if createDirectoryError := os.MkdirAll(filepath.Dir(full), 0o755); createDirectoryError != nil {
			continue
		}
		if info, statError := os.Stat(full); statError == nil && info.IsDir() {
			continue
		}
		if writeError := os.WriteFile(full, []byte(files[name]), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
}

// generatePatterns writes an ignore file's worth of lines from every construct: literals, `*`, `?`, sets,
// `**` in each position, anchoring, trailing slashes, negation, escapes, comments, blank lines and
// trailing spaces.
func generatePatterns(random *rand.Rand, names []string) string {
	pieces := []string{"*", "?", "**", "[ab]", "[!a]", "[a-c]", "[[:alpha:]]", "\\*", "\\?", "*.log", "*.tmp"}
	var lines []string
	for count := 0; count < 1+random.Intn(8); count++ {
		var segments []string
		for segment := 0; segment < 1+random.Intn(3); segment++ {
			if random.Intn(2) == 0 {
				segments = append(segments, pieces[random.Intn(len(pieces))])
			} else {
				segments = append(segments, escapeName(names[random.Intn(len(names))]))
			}
		}
		line := strings.Join(segments, "/")
		switch random.Intn(10) {
		case 0:
			line = "/" + line
		case 1:
			line += "/"
		case 2:
			line = "!" + line
		case 3:
			line = "# " + line
		case 4:
			line += "   "
		case 5:
			line += "\\ "
		case 6:
			line = ""
		case 7:
			line += "\r"
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n") + "\n"
}

// escapeName writes a file name as a pattern that matches it literally. Live glob characters come from
// the pieces generatePatterns mixes in.
func escapeName(name string) string {
	var escaped strings.Builder
	for index := 0; index < len(name); index++ {
		character := name[index]
		if strings.IndexByte(`*?[]\!#`, character) >= 0 {
			escaped.WriteByte('\\')
		}
		escaped.WriteByte(character)
	}
	return escaped.String()
}
