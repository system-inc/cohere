package gitignore

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
)

// The differential, recorded: on trees we wrote, every path's verdict and deciding rule equals git check-ignore's,
// read with the index git wrote for the tree, so a path the index tracks, or a directory holding one, is not ignored.
// git ran once, when the recordings were made, and never here: each tree is laid out from its txtar file, each of its
// repositories given the index git wrote, and the matcher is held to the answers git gave. How git made them, and the
// program that did, is in testdata/recorded/README.md.

// generatedConstructs is every construct the generator writes into an ignore file, each found by its lines.
var generatedConstructs = map[string]func(line string) bool{
	"a comment":                func(line string) bool { return strings.HasPrefix(line, "#") },
	"a blank line":             func(line string) bool { return line == "" },
	"a negation":               func(line string) bool { return strings.HasPrefix(line, "!") },
	"a leading slash":          func(line string) bool { return strings.HasPrefix(line, "/") },
	"a trailing slash":         func(line string) bool { return strings.HasSuffix(line, "/") },
	"a slash in the middle":    func(line string) bool { return strings.Contains(strings.Trim(line, "/ \r"), "/") },
	"a lone star":              func(line string) bool { return hasPatternSegment(line, "*") },
	"a lone question mark":     func(line string) bool { return hasPatternSegment(line, "?") },
	"a double star":            func(line string) bool { return strings.Contains(line, "**") },
	"a set":                    func(line string) bool { return strings.Contains(line, "[ab]") },
	"a negated set":            func(line string) bool { return strings.Contains(line, "[!a]") },
	"a range":                  func(line string) bool { return strings.Contains(line, "[a-c]") },
	"a character class":        func(line string) bool { return strings.Contains(line, "[[:alpha:]]") },
	"an escaped star":          func(line string) bool { return strings.Contains(line, `\*`) },
	"an escaped question mark": func(line string) bool { return strings.Contains(line, `\?`) },
	"an escaped bang or hash": func(line string) bool {
		return strings.Contains(line, `\!`) || strings.Contains(line, `\#`)
	},
	"trailing spaces":           func(line string) bool { return strings.HasSuffix(line, "   ") },
	"an escaped trailing space": func(line string) bool { return strings.HasSuffix(line, `\ `) },
	"a carriage return":         func(line string) bool { return strings.HasSuffix(line, "\r") },
}

// hasPatternSegment reports whether a pattern line, its negation, anchoring and trailing slash aside, holds segment
// as one of its slash-separated segments.
func hasPatternSegment(line string, segment string) bool {
	return slices.Contains(strings.Split(strings.Trim(line, "/!# \r"), "/"), segment)
}

// comparison is what comparing one repository's paths with git's answers found: how many of each kind of verdict, and
// where the two differ.
type comparison struct {
	compared, ignored, reincluded, undecided, ignoreFiles int
	// trackedUnderRule counts the paths the index tracks that the ignore files alone would ignore: the answer
	// reading the index changes. enteredUnderRule counts the directories among them, which a walk that prunes what
	// Ignored excludes enters for the tracked paths they hold, and trackedInsideExcluded the tracked paths found
	// inside a directory a rule excludes.
	trackedUnderRule, enteredUnderRule, trackedInsideExcluded int
	nested                                                    []string
	differences                                               []string
}

// add counts another comparison's paths into this one.
func (total *comparison) add(other comparison) {
	total.compared += other.compared
	total.ignored += other.ignored
	total.reincluded += other.reincluded
	total.undecided += other.undecided
	total.ignoreFiles += other.ignoreFiles
	total.trackedUnderRule += other.trackedUnderRule
	total.enteredUnderRule += other.enteredUnderRule
	total.trackedInsideExcluded += other.trackedInsideExcluded
	total.nested = append(total.nested, other.nested...)
	total.differences = append(total.differences, other.differences...)
}

// compareWithRecording decides every path in repository, laid out in the tree at root, with the matcher, and lists
// where it differs from git's recorded answers. Repositories nested in it are not entered; they are returned for their
// own comparison.
func compareWithRecording(
	test *testing.T,
	tree recordedTree,
	root string,
	repository recordedRepository,
) comparison {
	test.Helper()
	var result comparison
	repositoryRoot := filepath.Join(root, filepath.FromSlash(repository.path))
	matcher, newGitignoreError := New(repositoryRoot)
	if newGitignoreError != nil {
		test.Fatalf("%s: %v", repository.path, newGitignoreError)
	}
	// An index that fell back would leave the rules alone deciding, so the comparison would not be the one recorded.
	if fallback := matcher.IndexFallback(); fallback != nil {
		test.Fatalf("%s: the recorded index was not read: %v", repository.path, fallback)
	}
	if !slices.Equal(matcher.index.paths, repository.tracked) {
		test.Fatalf("%s: the recorded index tracks %q, and the tree's comment says %q", repository.path,
			matcher.index.paths, repository.tracked)
	}

	ours := map[string]verdict{}
	var paths []string
	var walk func(scope *Matcher, directory string)
	walk = func(scope *Matcher, directory string) {
		entries, readDirectoryError := os.ReadDir(filepath.Join(repositoryRoot, filepath.FromSlash(directory)))
		if readDirectoryError != nil {
			test.Fatalf("%s: %v", repository.path, readDirectoryError)
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
					filepath.Join(repositoryRoot, filepath.FromSlash(relative), ".git"),
				); statError == nil {
					result.nested = append(result.nested, treePath(repository.path, relative))
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
				if isDirectory {
					result.enteredUnderRule++
				}
			}
			if excluded, _ := scope.Excluded(); excluded && !isDirectory && matcher.index.tracks(relative) {
				result.trackedInsideExcluded++
			}
			if isDirectory {
				// Every directory is entered, an excluded one too: the trees are small, and below an excluded
				// directory each untracked path takes the rule that excluded it, which git answered for as well.
				child, enterError := scope.Enter(relative)
				if enterError != nil {
					test.Fatalf("%s: %v", repository.path, enterError)
				}
				walk(child, relative)
			}
		}
	}
	walk(matcher, "")

	literal, globbed := tree.answers(test, repository)
	sort.Strings(paths)
	for _, relative := range paths {
		mine := ours[relative]
		answers := literal
		if strings.ContainsAny(relative, globCharacters) {
			answers = globbed
		}
		gits, answered := answers[relative]
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
	if answered := len(literal) + len(globbed); answered != len(paths) {
		result.differences = append(
			result.differences,
			fmt.Sprintf("git answered for %d paths, %d were asked", answered, len(paths)),
		)
	}
	return result
}

// compareRecordedTree lays out the tree recorded in directory and compares each of its repositories, reporting every
// difference, and returns the counts across them.
func compareRecordedTree(test *testing.T, directory string) comparison {
	test.Helper()
	tree := readRecordedTree(test, directory)
	root := test.TempDir()
	tree.lay(test, root)
	var total comparison
	var nested []string
	for _, repository := range tree.repositories {
		result := compareWithRecording(test, tree, root, repository)
		name := filepath.Base(directory) + " " + repository.path
		test.Logf(
			"%s: %d paths compared (%d ignored, %d re-included by a negation, %d decided by no rule, %d tracked though a rule matches), %d ignore files read, %d nested repositories, %d differences",
			name,
			result.compared,
			result.ignored,
			result.reincluded,
			result.undecided,
			result.trackedUnderRule,
			result.ignoreFiles,
			len(result.nested),
			len(result.differences),
		)
		for index, difference := range result.differences {
			if index == 20 {
				test.Errorf("%s: %d more differences", name, len(result.differences)-20)
				break
			}
			test.Errorf("%s: %s", name, difference)
		}
		if result.compared == 0 {
			test.Errorf("%s: no path was compared, so agreeing proves nothing", name)
		}
		nested = append(nested, result.nested...)
		total.add(result)
	}
	// A nested repository the walk met that git was never asked about would go uncompared.
	var named []string
	for _, repository := range tree.repositories[1:] {
		named = append(named, repository.path)
	}
	sort.Strings(nested)
	if !slices.Equal(nested, named) {
		test.Errorf("the walk met the nested repositories %q, and the tree's comment names %q", nested, named)
	}
	return total
}

// Planted trees, each written for one way the index or the rules decide: a tracked path under a matching pattern is
// not ignored; a directory holding a tracked path is not ignored, and the walk enters it; a nested repository is
// judged as its own, by its own index, ignore files and info/exclude, the parent's rules not reaching in; and every
// path's deciding source, line and pattern, or none. Each case fails when its tree exercises nothing of what it is
// named for.
func TestTheMatcherAgreesWithGitOnPlantedTrees(test *testing.T) {
	test.Parallel()
	cases := map[string]struct {
		exercised func(total comparison) bool
		lacking   string
	}{
		"tracked-under-a-pattern": {
			exercised: func(total comparison) bool { return total.trackedUnderRule > total.enteredUnderRule },
			lacking:   "no tracked file a rule matches",
		},
		"directory-holding-a-tracked-path": {
			exercised: func(total comparison) bool {
				return total.enteredUnderRule > 0 && total.trackedInsideExcluded > 0
			},
			lacking: "no excluded directory entered for the tracked files it holds",
		},
		"nested-repository": {
			exercised: func(total comparison) bool { return len(total.nested) > 0 },
			lacking:   "no nested repository",
		},
		"deciding-sources": {
			exercised: func(total comparison) bool {
				return total.ignored > 0 && total.reincluded > 0 && total.undecided > 0
			},
			lacking: "no path ignored, re-included and decided by no rule",
		},
	}
	recorded := recordedCaseDirectories(test, "planted")
	var named []string
	for name := range cases {
		named = append(named, name)
	}
	sort.Strings(named)
	if !slices.Equal(recorded, named) {
		test.Fatalf("the planted trees recorded are %q, and the cases %q: each is compared by a case", recorded, named)
	}
	for name, testCase := range cases {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			total := compareRecordedTree(test, filepath.Join(recordedTrees, "planted", name))
			if !testCase.exercised(total) {
				test.Errorf("the tree exercises %s, so agreeing proves nothing of it", testCase.lacking)
			}
		})
	}
}

// The same differential over generated trees: random names, random ignore files at random depths, and patterns drawn
// from every construct gitignore(5) describes, so the matcher meets combinations no planted tree holds. Some files of
// each tree were added to its index whatever the patterns say, as `git add -f` adds them, so a tracked path under a
// matching pattern, and inside an excluded directory, is compared too. Each tree was generated once, from its seed,
// and recorded with git's answers; the generator is in the README, never run here.
func TestTheMatcherAgreesWithGitOnGeneratedTrees(test *testing.T) {
	test.Parallel()
	seeds := recordedCaseDirectories(test, "generated")
	var total comparison
	found := map[string]bool{}
	for _, seed := range seeds {
		directory := filepath.Join(recordedTrees, "generated", seed)
		total.add(compareRecordedTree(test, directory))
		for _, file := range readRecordedTree(test, directory).archive.Files {
			name := recordingName(test, file.Name)
			if filepath.Base(name) != IgnoreFileName && name != ExcludeFile {
				continue
			}
			for _, line := range strings.Split(strings.TrimSuffix(string(file.Data), "\n"), "\n") {
				for construct, appears := range generatedConstructs {
					if appears(line) {
						found[construct] = true
					}
				}
			}
		}
	}
	test.Logf(
		"%d generated trees, %d paths compared (%d ignored, %d re-included by a negation, %d tracked though a rule matches, %d tracked inside an excluded directory)",
		len(seeds),
		total.compared,
		total.ignored,
		total.reincluded,
		total.trackedUnderRule,
		total.trackedInsideExcluded,
	)
	// Trees whose rules decided nothing, or whose index tracked nothing a rule matched, would agree with git without
	// testing the matcher.
	if len(seeds) < 3 || total.ignored == 0 || total.reincluded == 0 || total.trackedUnderRule == 0 ||
		total.trackedInsideExcluded == 0 {
		test.Errorf(
			"the generated trees exercised too little: %d trees, %d ignored, %d re-included, %d tracked though a rule matches, %d tracked inside an excluded directory",
			len(seeds),
			total.ignored,
			total.reincluded,
			total.trackedUnderRule,
			total.trackedInsideExcluded,
		)
	}
	for construct := range generatedConstructs {
		if !found[construct] {
			test.Errorf("no generated ignore file holds %s", construct)
		}
	}
}
