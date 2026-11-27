# git's answers over trees we wrote

Every file here is real git's (2.54.0, Apple Git-157, run with no configuration of the machine's) answer over a tree
we wrote, recorded once so the tests in `internal/gitignore` compare the matcher with git without running it. The
object format is SHA-1. The stat data in each index (times, device, inode, owner) is the machine's that wrote it and
means nothing here; a reader takes only the mode, the flags and the path, so a recording made again differs from these
in those bytes alone.

## The trees: `planted/` and `generated/`

Each directory is one tree, read by `recorded_trees_test.go`:

- `tree.txtar` is the tree, every file and every empty directory (a name ending in `/`), and each repository's
  `.git/info/exclude` when it has one. A name that would not survive as it is, one ending in a space, which txtar trims
  from a file's name, or holding a backslash, is written Go-quoted. The archive's comment names each repository on a
  line `repository <path>` (`.` for the tree's root), followed by the paths its index tracks, one to a line, indented
  by a tab, quoted the same way.
- `repository.index` is the root repository's `.git/index`, and `repository-<path>.index` a nested repository's, its
  path's slashes as dashes.
- `repository.check-ignore` is `git check-ignore --verbose --non-matching --stdin` in that repository over each of its
  paths that holds none of `*?[\`, one per line, outside `.git` and any repository nested in it. check-ignore reads
  each path as a pathspec against the index, where `star*` would match a tracked `star*/deeper/x`, and refuses
  `--literal-pathspecs`, which would fix that; so a path holding one of those bytes is asked with `--no-index` instead,
  in `repository.check-ignore-no-index`, and the test gives it no rule when the index tracks it, or a path below it, as
  spelled.

`planted/` holds four trees written by hand, each for one way the index or the rules decide: a tracked path under a
matching pattern (`tracked-under-a-pattern`), a directory a rule excludes holding tracked paths
(`directory-holding-a-tracked-path`), a repository nested in another, judged by its own index, ignore files and
info/exclude (`nested-repository`), and the deciding source of every path, from each kind of file, or none
(`deciding-sources`).

`generated/` holds five trees the generator below wrote, one per seed: random names, ignore files at random depths
with patterns drawn from every construct gitignore(5) describes, and about a quarter of each tree's files added to its
index whatever the patterns say, as `git add -f` adds them. The seeds, 12, 23, 25, 26 and 54, were chosen from the
first sixty by `go run . survey 1 60`, so that between them the trees hold every construct, paths ignored and
re-included, and tracked files under a rule and inside an excluded directory, which the test requires of them.

The program below made both, run from its own directory (beside a `go.mod` reading `module recorder` and `go 1.27`)
with `go run . record <this directory> 12 23 25 26 54`. It writes the planted trees from the plans in it, and each
generated tree from its seed; for every tree it makes each repository with `git init --template=`, adds the tracked
paths with `git update-index --add`, which reads each path literally, and asks check-ignore of every path.

```go
// Records git's answers over trees we wrote, for internal/gitignore's recorded tests: each tree as a txtar
// file, each repository's .git/index as git wrote it, and git check-ignore's verbose answer for every path.
//
//	go run . record <testdata/recorded> <seed>...
//	go run . survey <first seed> <last seed>
package main

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// repositoryPlan is one repository of a tree: its directory relative to the tree, "." for the root, and the
// paths its index tracks, relative to it.
type repositoryPlan struct {
	path    string
	tracked []string
}

// treePlan is a planted tree: its files (a name ending in "/" is a directory), including any
// `.git/info/exclude`, and its repositories, the root first.
type treePlan struct {
	name         string
	comment      string
	files        map[string]string
	repositories []repositoryPlan
}

var plantedTrees = []treePlan{
	{
		name: "tracked-under-a-pattern",
		comment: "gitignore(5): \"Files already tracked by Git are not affected\". value.go is tracked under `*.go`, as\n" +
			"`git add -f` tracks it, and is not ignored; its untracked sibling is ignored by the pattern.\n",
		files: map[string]string{
			".gitignore": "*.go\n",
			"value.go":   "package value\n",
			"sibling.go": "package value\n",
			"notes.txt":  "notes\n",
			"other.txt":  "other\n",
		},
		repositories: []repositoryPlan{{path: ".", tracked: []string{".gitignore", "notes.txt", "value.go"}}},
	},
	{
		name: "directory-holding-a-tracked-path",
		comment: "A directory a rule excludes that holds a tracked path is not ignored, so a walk enters it; beside the\n" +
			"tracked files, the untracked ones read ignored by the rule that excluded the directory, and a directory\n" +
			"there holding nothing tracked is ignored.\n",
		files: map[string]string{
			".gitignore":          "build/\n",
			"main.go":             "package main\n",
			"build/kept.go":       "package build\n",
			"build/scratch.go":    "package build\n",
			"build/deep/inner.go": "package deep\n",
			"build/deep/loose.go": "package deep\n",
			"build/cache/blob":    "blob\n",
		},
		repositories: []repositoryPlan{
			{path: ".", tracked: []string{".gitignore", "build/deep/inner.go", "build/kept.go", "main.go"}},
		},
	},
	{
		name: "nested-repository",
		comment: "library is a repository of its own, judged by its own ignore files, info/exclude and index: the\n" +
			"parent's `*.log` and `vendor/` do not reach into it.\n",
		files: map[string]string{
			".gitignore":                "*.log\nvendor/\n",
			"main.go":                   "package main\n",
			"app.log":                   "log\n",
			"vendor/module.go":          "package module\n",
			"library/.git/info/exclude": "# library's own\nlocal-only\n",
			"library/.gitignore":        "*.tmp\n",
			"library/debug.log":         "log\n",
			"library/scratch.tmp":       "scratch\n",
			"library/kept.tmp":          "kept\n",
			"library/local-only":        "local\n",
			"library/vendor/module.go":  "package module\n",
		},
		repositories: []repositoryPlan{
			{path: ".", tracked: []string{".gitignore", "main.go"}},
			{path: "library", tracked: []string{".gitignore", "kept.tmp"}},
		},
	},
	{
		name: "deciding-sources",
		comment: "Every path's deciding source, line and pattern, from a `.gitignore`, a nested one, or info/exclude, and\n" +
			"none for a path no rule matches.\n",
		files: map[string]string{
			".git/info/exclude": "# repository-wide\nper-repository\nkept-by-gitignore\n",
			".gitignore": "# comments and blank lines count as lines\n\n*.log\n!keep.log\n/anchored\ndocs/generated\n" +
				"**/deep/x\n\\#literal\nescaped\\ \ntrailing   \n!kept-by-gitignore\n",
			"sub/.gitignore":       "!*.log\nlocal\n/rooted\n",
			"app.log":              "",
			"keep.log":             "",
			"anchored":             "",
			"docs/generated":       "",
			"docs/written":         "",
			"a/b/deep/x":           "",
			"#literal":             "",
			"escaped ":             "",
			"trailing":             "",
			"per-repository":       "",
			"kept-by-gitignore":    "",
			"plain.txt":            "",
			"sub/app.log":          "",
			"sub/anchored":         "",
			"sub/local":            "",
			"sub/rooted":           "",
			"sub/inner/rooted":     "",
			"sub/inner/local":      "",
			"sub/docs/generated":   "",
			"sub/per-repository":   "",
			"sub/inner/plain.txt":  "",
			"sub/inner/.gitignore": "*.txt\n",
		},
		repositories: []repositoryPlan{
			{path: ".", tracked: []string{".gitignore", "plain.txt", "sub/.gitignore", "sub/inner/.gitignore"}},
		},
	},
}

func main() {
	if len(os.Args) < 3 {
		fail(errors.New("usage: record <testdata/recorded> <seed>... | survey <first seed> <last seed>"))
	}
	scratch, scratchError := os.MkdirTemp("", "gitignore-recorder-")
	if scratchError != nil {
		fail(scratchError)
	}
	scratch, _ = filepath.EvalSymlinks(scratch)
	defer os.RemoveAll(scratch)
	switch os.Args[1] {
	case "record":
		output := os.Args[2]
		for _, plan := range plantedTrees {
			root := filepath.Join(scratch, "planted", plan.name)
			layPlanted(scratch, root, plan)
			record(scratch, root, filepath.Join(output, "planted", plan.name), plan.comment, plan.repositories)
		}
		for _, argument := range os.Args[3:] {
			seed, parseError := strconv.ParseInt(argument, 10, 64)
			if parseError != nil {
				fail(parseError)
			}
			root := filepath.Join(scratch, "generated", argument)
			tracked := layGenerated(scratch, root, seed)
			comment := fmt.Sprintf("Generated from seed %d: random names, ignore files at random depths, and about a quarter of\n"+
				"the files tracked whatever the patterns say, as `git add -f` tracks them.\n", seed)
			record(scratch, root, filepath.Join(output, "generated", "seed-"+argument), comment,
				[]repositoryPlan{{path: ".", tracked: tracked}})
		}
	case "survey":
		first, firstError := strconv.ParseInt(os.Args[2], 10, 64)
		must(firstError)
		last, lastError := strconv.ParseInt(os.Args[3], 10, 64)
		must(lastError)
		for seed := first; seed <= last; seed++ {
			root := filepath.Join(scratch, "survey", strconv.FormatInt(seed, 10))
			tracked := layGenerated(scratch, root, seed)
			survey(scratch, root, seed, tracked)
		}
	}
}

func fail(problem error) {
	fmt.Fprintln(os.Stderr, problem)
	os.Exit(1)
}

// git runs git in directory with no configuration of the machine's, and returns its standard output.
// check-ignore's exit 1, nothing ignored, is an answer.
func git(scratch string, directory string, stdin []byte, arguments ...string) []byte {
	command := exec.Command("git", append([]string{
		"-c", "core.autocrlf=false", "-c", "commit.gpgsign=false", "-c", "init.defaultBranch=main",
		"-c", "core.excludesFile=/dev/null", "-c", "core.ignorecase=false",
	}, arguments...)...)
	command.Dir = directory
	command.Env = []string{
		"PATH=/usr/bin:/bin", "HOME=" + scratch, "XDG_CONFIG_HOME=" + scratch,
		"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.invalid", "GIT_AUTHOR_DATE=1700000000 +0000",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.invalid", "GIT_COMMITTER_DATE=1700000000 +0000",
	}
	command.Stdin = bytes.NewReader(stdin)
	var standardError bytes.Buffer
	command.Stderr = &standardError
	output, runError := command.Output()
	var exitError *exec.ExitError
	if errors.As(runError, &exitError) && exitError.ExitCode() == 1 && arguments[0] == "check-ignore" {
		runError = nil
	}
	if runError != nil {
		fail(fmt.Errorf("git %s in %s: %v\n%s", strings.Join(arguments, " "), directory, runError, standardError.String()))
	}
	return output
}

// isGitPath reports whether a tree's name lies inside a `.git` directory.
func isGitPath(name string) bool {
	return name == ".git" || strings.HasPrefix(name, ".git/") || strings.Contains(name, "/.git/")
}

// layPlanted writes a planted tree, makes each of its repositories, writes the `.git` files the plan holds,
// and adds each repository's tracked paths to its index.
func layPlanted(scratch string, root string, plan treePlan) {
	writeFiles(root, plan.files, func(name string) bool { return !isGitPath(name) })
	for _, repository := range plan.repositories {
		git(scratch, root, nil, "init", "-q", "--template=", repository.path)
	}
	writeFiles(root, plan.files, isGitPath)
	for _, repository := range plan.repositories {
		track(scratch, filepath.Join(root, filepath.FromSlash(repository.path)), repository.tracked)
	}
}

func writeFiles(root string, files map[string]string, include func(string) bool) {
	for name, contents := range files {
		if !include(name) {
			continue
		}
		full := filepath.Join(root, filepath.FromSlash(name))
		if strings.HasSuffix(name, "/") {
			must(os.MkdirAll(full, 0o755))
			continue
		}
		must(os.MkdirAll(filepath.Dir(full), 0o755))
		must(os.WriteFile(full, []byte(contents), 0o644))
	}
}

func must(problem error) {
	if problem != nil {
		fail(problem)
	}
}

// track adds paths to the index of the repository at directory, each read literally, as update-index reads it,
// where `git add` would read `star*` as a pattern.
func track(scratch string, directory string, paths []string) {
	if len(paths) == 0 {
		return
	}
	var input bytes.Buffer
	for _, path := range paths {
		input.WriteString(path)
		input.WriteByte(0)
	}
	git(scratch, directory, input.Bytes(), "update-index", "--add", "-z", "--stdin")
}

// layGenerated writes seed's generated tree, makes its repository, and tracks some of its files, returning them.
func layGenerated(scratch string, root string, seed int64) []string {
	random := rand.New(rand.NewSource(seed))
	exclude := generateTree(random, root)
	git(scratch, root, nil, "init", "-q", "--template=")
	if exclude != "" {
		writeFiles(root, map[string]string{".git/info/exclude": exclude}, func(string) bool { return true })
	}
	return trackRandomFiles(scratch, random, root)
}

// generateTree writes a random tree with random ignore files under root, and returns patterns for its
// info/exclude, or "" for none.
func generateTree(random *rand.Rand, root string) string {
	names := []string{"a", "b", "ab", "abc", "x.log", "y.tmp", "build", "out", "node_modules", "keep",
		"#hash", "!bang", "sp ace", "trail ", "star*", "q?", "br[ack]et", "back\\slash", "deep", "Dot.go", ".hidden"}
	directories := []string{""}
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
			files[joinRelative(directory, ".gitignore")] = generatePatterns(random, names)
		}
	}
	writeTreeTolerant(root, files)
	if random.Intn(2) == 0 {
		return generatePatterns(random, names)
	}
	return ""
}

func joinRelative(directory string, name string) string {
	if directory == "" {
		return name
	}
	return directory + "/" + name
}

// writeTreeTolerant writes a generated tree, skipping a file whose name a directory already took.
func writeTreeTolerant(root string, files map[string]string) {
	keys := make([]string, 0, len(files))
	for name := range files {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	for _, name := range keys {
		full := filepath.Join(root, filepath.FromSlash(name))
		if strings.HasSuffix(name, "/") {
			_ = os.MkdirAll(full, 0o755)
			continue
		}
		if createDirectoryError := os.MkdirAll(filepath.Dir(full), 0o755); createDirectoryError != nil {
			continue
		}
		if info, statError := os.Stat(full); statError == nil && info.IsDir() {
			continue
		}
		must(os.WriteFile(full, []byte(files[name]), 0o644))
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

// escapeName writes a file name as a pattern that matches it literally.
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

// trackRandomFiles tracks about a quarter of a generated tree's files, ignored or not.
func trackRandomFiles(scratch string, random *rand.Rand, root string) []string {
	var paths []string
	must(filepath.WalkDir(root, func(path string, entry fs.DirEntry, pathError error) error {
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
		paths = append(paths, filepath.ToSlash(relative))
		return nil
	}))
	track(scratch, root, paths)
	return paths
}

// quoteName writes a name as the recordings write it: as it is, or Go-quoted when it would not survive as it
// is, with a space at either end, a backslash, a quote, or a byte that is not printable.
func quoteName(name string) string {
	if strings.TrimSpace(name) != name || strings.HasPrefix(name, `"`) || strconv.Quote(name) != `"`+name+`"` {
		return strconv.Quote(name)
	}
	return name
}

// repositoryName is the base name of a repository's recordings: "repository" for the root, and
// "repository-" and its path, slashes as dashes, for one nested in it.
func repositoryName(path string) string {
	if path == "." {
		return "repository"
	}
	return "repository-" + strings.ReplaceAll(path, "/", "-")
}

// record writes the tree at root as tree.txtar in output, and for each repository its index and git
// check-ignore's verbose answer for each of its paths.
func record(scratch string, root string, output string, comment string, repositories []repositoryPlan) {
	must(os.MkdirAll(output, 0o755))
	var archive bytes.Buffer
	archive.WriteString(comment)
	archive.WriteString("\n")
	for _, repository := range repositories {
		archive.WriteString("repository " + repository.path + "\n")
		sorted := append([]string(nil), repository.tracked...)
		sort.Strings(sorted)
		for _, path := range sorted {
			archive.WriteString("\t" + quoteName(path) + "\n")
		}
	}
	var names []string
	contents := map[string][]byte{}
	must(filepath.WalkDir(root, func(path string, entry fs.DirEntry, pathError error) error {
		if pathError != nil || path == root {
			return pathError
		}
		relative, _ := filepath.Rel(root, path)
		relative = filepath.ToSlash(relative)
		if entry.IsDir() && entry.Name() == ".git" {
			// Only a repository's info/exclude is the tree's; the rest is git's own.
			exclude, readError := os.ReadFile(filepath.Join(path, "info", "exclude"))
			if readError == nil {
				names = append(names, relative+"/info/exclude")
				contents[relative+"/info/exclude"] = exclude
			}
			return filepath.SkipDir
		}
		if entry.IsDir() {
			children, _ := os.ReadDir(path)
			if len(children) == 0 {
				names = append(names, relative+"/")
			}
			return nil
		}
		data, readError := os.ReadFile(path)
		names = append(names, relative)
		contents[relative] = data
		return readError
	}))
	sort.Strings(names)
	for _, name := range names {
		data := contents[name]
		if len(data) > 0 && !bytes.HasSuffix(data, []byte("\n")) {
			fail(fmt.Errorf("%s does not end in a newline, which a txtar file cannot hold", name))
		}
		archive.WriteString("-- " + quoteName(name) + " --\n")
		archive.Write(data)
	}
	must(os.WriteFile(filepath.Join(output, "tree.txtar"), archive.Bytes(), 0o644))

	for _, repository := range repositories {
		directory := filepath.Join(root, filepath.FromSlash(repository.path))
		name := repositoryName(repository.path)
		index, readError := os.ReadFile(filepath.Join(directory, ".git", "index"))
		must(readError)
		must(os.WriteFile(filepath.Join(output, name+".index"), index, 0o644))
		literal, globbed := repositoryPaths(directory)
		must(os.WriteFile(filepath.Join(output, name+".check-ignore"),
			checkIgnore(scratch, directory, literal), 0o644))
		if len(globbed) > 0 {
			must(os.WriteFile(filepath.Join(output, name+".check-ignore-no-index"),
				checkIgnore(scratch, directory, globbed, "--no-index"), 0o644))
		}
	}
}

// repositoryPaths is every path in the repository at directory, sorted, outside `.git` and any repository
// nested in it: those holding none of `*?[\`, which check-ignore asks of the index literally, and those
// holding one, which it would read as a glob against the index.
func repositoryPaths(directory string) (literal []string, globbed []string) {
	must(filepath.WalkDir(directory, func(path string, entry fs.DirEntry, pathError error) error {
		if pathError != nil || path == directory {
			return pathError
		}
		if entry.Name() == ".git" {
			return filepath.SkipDir
		}
		if entry.IsDir() {
			if _, statError := os.Lstat(filepath.Join(path, ".git")); statError == nil {
				return filepath.SkipDir
			}
		}
		relative, _ := filepath.Rel(directory, path)
		relative = filepath.ToSlash(relative)
		if strings.ContainsAny(relative, `*?[\`) {
			globbed = append(globbed, relative)
		} else {
			literal = append(literal, relative)
		}
		return nil
	}))
	sort.Strings(literal)
	sort.Strings(globbed)
	return literal, globbed
}

// checkIgnore is `git check-ignore --verbose --non-matching --stdin` over paths, one per line.
func checkIgnore(scratch string, directory string, paths []string, arguments ...string) []byte {
	var input bytes.Buffer
	for _, path := range paths {
		if strings.HasPrefix(path, `"`) || strings.ContainsAny(path, "\n") {
			fail(fmt.Errorf("%q cannot be written as one line of check-ignore's input", path))
		}
		input.WriteString(path + "\n")
	}
	return git(scratch, directory, input.Bytes(),
		append([]string{"check-ignore", "--verbose", "--non-matching", "--stdin"}, arguments...)...)
}

// constructs is every gitignore(5) construct the generator writes, each found in an ignore file's lines.
var constructs = map[string]func(line string) bool{
	"a comment":                func(line string) bool { return strings.HasPrefix(line, "#") },
	"a blank line":             func(line string) bool { return line == "" },
	"a negation":               func(line string) bool { return strings.HasPrefix(line, "!") },
	"a leading slash":          func(line string) bool { return strings.HasPrefix(line, "/") },
	"a trailing slash":         func(line string) bool { return strings.HasSuffix(line, "/") },
	"a slash in the middle":    func(line string) bool { return strings.Contains(strings.Trim(line, "/ \r"), "/") },
	"a lone star":              func(line string) bool { return hasSegment(line, "*") },
	"a lone question mark":     func(line string) bool { return hasSegment(line, "?") },
	"a double star":            func(line string) bool { return strings.Contains(line, "**") },
	"a set":                    func(line string) bool { return strings.Contains(line, "[ab]") },
	"a negated set":            func(line string) bool { return strings.Contains(line, "[!a]") },
	"a range":                  func(line string) bool { return strings.Contains(line, "[a-c]") },
	"a character class":        func(line string) bool { return strings.Contains(line, "[[:alpha:]]") },
	"an escaped star":          func(line string) bool { return strings.Contains(line, `\*`) },
	"an escaped question mark": func(line string) bool { return strings.Contains(line, `\?`) },
	"an escaped bang or hash":  func(line string) bool { return strings.Contains(line, `\!`) || strings.Contains(line, `\#`) },
	"trailing spaces":          func(line string) bool { return strings.HasSuffix(line, "   ") },
	"an escaped trailing space": func(line string) bool {
		return strings.HasSuffix(line, `\ `)
	},
	"a carriage return": func(line string) bool { return strings.HasSuffix(line, "\r") },
}

func hasSegment(line string, segment string) bool {
	for _, piece := range strings.Split(strings.Trim(line, "/!# \r"), "/") {
		if piece == segment {
			return true
		}
	}
	return false
}

// survey prints what one seed's tree exercises, for choosing the seeds to record.
func survey(scratch string, root string, seed int64, tracked []string) {
	found := map[string]bool{}
	files := 0
	must(filepath.WalkDir(root, func(path string, entry fs.DirEntry, pathError error) error {
		if pathError != nil {
			return pathError
		}
		if entry.Name() != ".gitignore" && !strings.HasSuffix(path, ".git/info/exclude") {
			return nil
		}
		files++
		data, _ := os.ReadFile(path)
		for _, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
			for name, matches := range constructs {
				if matches(line) {
					found[name] = true
				}
			}
		}
		return nil
	}))
	literal, globbed := repositoryPaths(root)
	all := append(append([]string{}, literal...), globbed...)
	answers := checkIgnore(scratch, root, all, "--no-index")
	ignoredByRules := map[string]bool{}
	ignored, reincluded := 0, 0
	for _, line := range strings.Split(strings.TrimSuffix(string(answers), "\n"), "\n") {
		source, path, _ := strings.Cut(line, "\t")
		if source == "::" || line == "" {
			continue
		}
		if strings.Contains(source, ":!") {
			reincluded++
			continue
		}
		ignored++
		ignoredByRules[strings.Trim(path, `"`)] = true
	}
	underRule, insideExcluded := 0, 0
	for _, path := range tracked {
		if ignoredByRules[path] {
			underRule++
		}
		for parent := filepath.Dir(path); parent != "."; parent = filepath.Dir(parent) {
			if ignoredByRules[filepath.ToSlash(parent)] {
				insideExcluded++
				break
			}
		}
	}
	var missing []string
	for name := range constructs {
		if !found[name] {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	fmt.Printf("seed %d: %d paths, %d ignore files, %d tracked, %d ignored, %d re-included, %d tracked under a rule, %d tracked inside an excluded directory, missing %q\n",
		seed, len(all), files, len(tracked), ignored, reincluded, underRule, insideExcluded, missing)
}
```

## The digest steps: `digest/`

Each `.index` is the index git wrote at one step of what a digest test, in `matcher_tracked_ignored_digest_test.go`,
takes a repository through. Every step starts from the same repository: a `.gitignore` excluding `*.log` and `build/`,
a committed `main.go`, an untracked `debug.log`, and `build/out.txt`.

- `committed.index`: `.gitignore` and `main.go` added and committed.
- `forced-log.index`: then `git add -f debug.log`.
- `removed-log.index`: then `git rm --cached debug.log`, tracking what `committed.index` does.
- `refreshed-by-status.index`: then `git status`, after main.go's time moved, which rewrites the index's stat data and
  tracks nothing new.
- `added-sources.index`: then `git add` of a changed `main.go` and a new `new.go`.
- `gitlink-at-log.index`: from `committed.index`, a gitlink at `debug.log`, and `gitlink-at-log-and-module.index` a
  second at `modules/library`, each naming the commit.
- `forced-log-and-service-log.index`: from `committed.index`, `git add -f` of `debug.log` and `service/trace.log`.
- `packages.index`: twenty-four package directories, each a source file, an ignored log, and a nested `.gitignore`
  re-including a `keep.log`, added with `git add -A`, then two of the logs with `git add -f`.
- A worktree added beside the repository with `worktree.useRelativePaths`, so nothing in it names a temporary path:
  `worktree.git` is its `.git` file, `worktree-gitdir` and `worktree-commondir` the files git wrote in its git
  directory, `worktree.index` its index as checked out and `worktree-forced-secret.index` after `git add -f secret.go`.
  `worktree.check-ignore` is `git check-ignore -v secret.go` in the worktree, with the main checkout's exclude file
  listing `secret.go`; git names that file by its absolute path, whose main checkout the recording writes `<main>`.

They were made with this script, run with `sh make-digest-fixtures.sh <directory>`:

```sh
set -eu
out=$1
work=$(mktemp -d)
work=$(cd "$work" && pwd -P)
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1 HOME="$work" XDG_CONFIG_HOME="$work"
export GIT_AUTHOR_NAME=t GIT_AUTHOR_EMAIL=t@example.invalid GIT_AUTHOR_DATE='1700000000 +0000'
export GIT_COMMITTER_NAME=t GIT_COMMITTER_EMAIL=t@example.invalid GIT_COMMITTER_DATE='1700000000 +0000'
git() {
	command git -c core.autocrlf=false -c commit.gpgsign=false -c init.defaultBranch=main \
		-c core.excludesFile=/dev/null -c core.ignorecase=false -c worktree.useRelativePaths=true "$@"
}
# base makes the repository every digest test starts from, in $work/$1.
base() {
	git init -q --template= "$work/$1"
	cd "$work/$1"
	printf '*.log\nbuild/\n' > .gitignore
	printf 'package main\n' > main.go
	printf 'a log\n' > debug.log
	mkdir build && printf 'built\n' > build/out.txt
	git add .gitignore main.go
	git commit -q -m first
}
save() { cp .git/index "$out/$1.index"; }

# What the index decides, step by step.
base follows
save committed
git add -f debug.log
save forced-log
git rm -q --cached debug.log
save removed-log
touch -t 202001010000 main.go
git status --porcelain > /dev/null
save refreshed-by-status
printf 'package main\n\nfunc main() {}\n' > main.go
printf 'package main\n' > new.go
git add main.go new.go
save added-sources

# A gitlink where the ignored file was, then one the rules do not exclude.
base gitlink
commit=$(git rev-parse HEAD)
git update-index --add --cacheinfo "160000,$commit,debug.log"
save gitlink-at-log
git update-index --add --cacheinfo "160000,$commit,modules/library"
save gitlink-at-log-and-module

# An ignored file tracked inside a scope.
base scope
mkdir service && printf 'trace\n' > service/trace.log
git add -f debug.log service/trace.log
save forced-log-and-service-log

# Twenty-four packages, two of their ignored logs tracked.
base packages
for number in $(seq 0 23); do
	mkdir -p "package$number/nested"
	printf 'package package%s\n' "$number" > "package$number/source.go"
	printf 'trace\n' > "package$number/trace.log"
	printf '!keep.log\n' > "package$number/nested/.gitignore"
	printf 'kept\n' > "package$number/nested/keep.log"
done
git add -A
git add -f package3/trace.log package17/trace.log
save packages

# A worktree, written with relative paths, reading the exclude file it shares with its main checkout.
base main
git worktree add --quiet ../worktree
mkdir -p .git/info
printf '# shared\nsecret.go\n' > .git/info/exclude
cp ../worktree/.git "$out/worktree.git"
cp .git/worktrees/worktree/gitdir "$out/worktree-gitdir"
cp .git/worktrees/worktree/commondir "$out/worktree-commondir"
cd ../worktree
printf 'package main\n' > secret.go
printf 'package main\n' > plain.go
cp "$(git rev-parse --git-dir)/index" "$out/worktree.index"
git check-ignore -v secret.go | sed "s|^$work/main/|<main>/|" > "$out/worktree.check-ignore"
git add -f secret.go
cp "$(git rev-parse --git-dir)/index" "$out/worktree-forced-secret.index"
```
