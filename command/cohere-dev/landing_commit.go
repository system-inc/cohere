package main

import (
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"
)

// A landing is one merge commit onto main, `Land #<task>: <what landed>`, whose first parent is main and whose
// second is the branch's head (#4v9pmef). Land used to merge main into the branch and fast-forward main onto
// it, which made the branch's whole history main's first-parent line: 114 of main's last 300 first-parent
// commits were merges like "Merge main into the work being landed", main read backwards, and `--first-parent`
// walked a branch's insides, two commits that never built among them (#ry0vkmq). Now the branch's history,
// its merges of main included, stays one hop off main's line, and the line reads as one landing per unit.
//
// A landing's commits are held to the subject the house writes, `area: what changed (#task)`, up to
// subjectLimit characters, the story in the body, since those subjects are what a reader of the branch sees.

// subjectLimit is the longest subject a landing or any commit it brings may have.
const subjectLimit = 72

// landingOptions are what land is told beyond the worktree it runs in.
type landingOptions struct {
	// Task is the task the landing finishes, without its #, when the branch's subjects don't name one alone.
	Task string
	// Message says what landed, when the branch holds more than one commit or its one subject won't fit.
	Message string
}

// parseLandingOptions reads `--task <id>` and `--message <what landed>`, each also as `--name=value`.
func parseLandingOptions(arguments []string) (landingOptions, error) {
	options := landingOptions{}
	for index := 0; index < len(arguments); index++ {
		name, value, joined := strings.Cut(strings.TrimLeft(arguments[index], "-"), "=")
		if !strings.HasPrefix(arguments[index], "--") || (name != "task" && name != "message") {
			return landingOptions{}, fmt.Errorf("land takes --task <id> and --message <what landed>, and %q is neither",
				arguments[index])
		}
		if !joined {
			if index+1 == len(arguments) {
				return landingOptions{}, fmt.Errorf("--%s needs a value", name)
			}
			index++
			value = arguments[index]
		}
		if name == "task" {
			options.Task = strings.TrimPrefix(value, "#")
		} else {
			options.Message = strings.TrimSpace(value)
		}
	}
	return options, nil
}

// branchCommit is one commit a landing brings to main.
type branchCommit struct {
	Commit  string
	Subject string
	Merge   bool
}

// branchCommits are the commits head holds and main does not, oldest first.
func branchCommits(worktree string, mainCommit string, head string) ([]branchCommit, error) {
	// %x00 between fields and %x01 between commits, so a subject is read whole.
	listing, gitOutputError := gitOutput(
		worktree,
		"log",
		"--reverse",
		"--format=%H%x00%P%x00%s%x01",
		mainCommit+".."+head,
	)
	if gitOutputError != nil {
		return nil, fmt.Errorf("listing the commits being landed: %w", gitOutputError)
	}
	commits := []branchCommit{}
	for _, record := range strings.Split(listing, "\x01") {
		fields := strings.Split(strings.TrimSpace(record), "\x00")
		if len(fields) != 3 {
			continue
		}
		commits = append(commits, branchCommit{
			Commit:  fields[0],
			Subject: fields[2],
			Merge:   len(strings.Fields(fields[1])) > 1,
		})
	}
	return commits, nil
}

// subjectVocabulary is every area a subject may name before its colon, read from a commit's tree: each
// directory's and submodule's name and its path from the root, and each top-level file's name without its
// extension, lowercased, as `contributing` for `CONTRIBUTING.md`. Read from the tree rather than kept as a list,
// so a folder that is added or renamed is an area the moment it lands, and one removed stops being one.
func subjectVocabulary(worktree string, commit string) (map[string]bool, error) {
	listing, gitOutputError := gitOutput(worktree, "ls-tree", "-r", "-t", "-z", commit)
	if gitOutputError != nil {
		return nil, fmt.Errorf("reading the tree to name its areas: %w", gitOutputError)
	}
	vocabulary := map[string]bool{}
	for _, entry := range strings.Split(listing, "\x00") {
		description, entryPath, found := strings.Cut(entry, "\t")
		if !found {
			continue
		}
		fields := strings.Fields(description)
		if len(fields) < 2 {
			continue
		}
		switch fields[1] {
		case "tree", "commit":
			vocabulary[entryPath] = true
			vocabulary[path.Base(entryPath)] = true
		case "blob":
			if !strings.Contains(entryPath, "/") {
				vocabulary[strings.ToLower(strings.TrimSuffix(entryPath, path.Ext(entryPath)))] = true
			}
		}
	}
	return vocabulary, nil
}

// knowsArea reports whether a subject's area is in the vocabulary: a name it holds, or a slash path ending a
// directory's path, as `release/dispatch` for internal/release/dispatch.
func knowsArea(vocabulary map[string]bool, area string) bool {
	if vocabulary[area] {
		return true
	}
	if !strings.Contains(area, "/") {
		return false
	}
	for known := range vocabulary {
		if strings.HasSuffix(known, "/"+area) {
			return true
		}
	}
	return false
}

// nearestArea is a folder whose name is the area with its separators taken out, `go_engine` for `goengine`,
// or empty when none is: the likeliest meaning of an area the vocabulary does not hold.
func nearestArea(vocabulary map[string]bool, area string) string {
	plain := func(name string) string {
		return strings.NewReplacer("_", "", "-", "", " ", "").Replace(strings.ToLower(name))
	}
	names := []string{}
	for known := range vocabulary {
		if !strings.Contains(known, "/") && known != area && plain(known) == plain(area) {
			names = append(names, known)
		}
	}
	slices.Sort(names)
	if len(names) == 0 {
		return ""
	}
	return names[0]
}

// checkSubjects refuses a landing whose commits break the house's subject: one over subjectLimit characters,
// or, for a commit that is not a merge, one without an area the vocabulary holds before its colon. Every
// commit that breaks it is named, with its subject and why, so one refusal says everything to fix.
func checkSubjects(commits []branchCommit, vocabulary map[string]bool) error {
	var problems []string
	for _, commit := range commits {
		var why []string
		if length := len([]rune(commit.Subject)); length > subjectLimit {
			why = append(why, fmt.Sprintf("%d characters, over %d", length, subjectLimit))
		}
		if !commit.Merge {
			area, _, found := strings.Cut(commit.Subject, ": ")
			switch {
			case !found:
				why = append(why, "no `area: ` before what changed")
			case !knowsArea(vocabulary, area):
				reason := fmt.Sprintf("%q names no folder or top-level file in the tree", area)
				if nearest := nearestArea(vocabulary, area); nearest != "" {
					reason += fmt.Sprintf(" (%q is one)", nearest)
				}
				why = append(why, reason)
			}
		}
		if len(why) > 0 {
			problems = append(problems, fmt.Sprintf("  %s %s\n    %s", shortCommit(commit.Commit), commit.Subject,
				strings.Join(why, "; ")))
		}
	}
	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf("a landing's commits read `area: what changed (#task)`, up to %d characters, with the story in "+
		"the body, and the area a folder or top-level file in the tree. These don't:\n%s\nCommit the same changes "+
		"again on a new branch from main, each with a subject that fits (`git diff main...<branch> | git apply` "+
		"brings them over), and land that branch", subjectLimit, strings.Join(problems, "\n"))
}

// taskReference is a task named in a subject, `#` and its id.
var taskReference = regexp.MustCompile(`#([0-9a-z][0-9a-z_]{1,63})\b`)

// landingTask is the task a landing finishes: the one given, or the one the branch's subjects name, merges
// aside. A branch that names none, or several, is refused unless one is given.
func landingTask(commits []branchCommit, given string) (string, error) {
	if given != "" {
		return given, nil
	}
	named := []string{}
	for _, commit := range commits {
		if commit.Merge {
			continue
		}
		for _, match := range taskReference.FindAllStringSubmatch(commit.Subject, -1) {
			if !slices.Contains(named, match[1]) {
				named = append(named, match[1])
			}
		}
	}
	switch len(named) {
	case 1:
		return named[0], nil
	case 0:
		return "", fmt.Errorf("no commit being landed names its task in its subject, as `(#task)`: name the task " +
			"this landing finishes with --task <id>")
	default:
		return "", fmt.Errorf("the commits being landed name %d tasks, #%s: name the one this landing finishes "+
			"with --task <id>", len(named), strings.Join(named, ", #"))
	}
}

// landingSubject is `Land #<task>: <what landed>`, what landed being the message given, or the branch's one
// commit's subject without the task it names, which the landing's subject names already. Over subjectLimit,
// or with several commits and no message, it is refused, asking for one.
func landingSubject(commits []branchCommit, task string, message string) (string, error) {
	what := message
	if what == "" {
		var own []branchCommit
		for _, commit := range commits {
			if !commit.Merge {
				own = append(own, commit)
			}
		}
		if len(own) != 1 {
			return "", fmt.Errorf("the branch brings %d commits, so say what landed in one line with --message "+
				"\"<what landed>\", for the subject `Land #%s: <what landed>`", len(own), task)
		}
		what = strings.TrimSpace(strings.ReplaceAll(own[0].Subject, "(#"+task+")", ""))
	}
	subject := "Land #" + task + ": " + what
	if length := len([]rune(subject)); length > subjectLimit {
		return "", fmt.Errorf("the landing's subject would be %q, %d characters, over %d: say what landed in "+
			"fewer words with --message", subject, length, subjectLimit)
	}
	return subject, nil
}

// landingMessage is the landing commit's whole message: its subject, the commits it brings with the branch
// and main it joins, and the gate's account.
func landingMessage(
	subject string,
	branch string,
	head string,
	mainCommit string,
	commits []branchCommit,
	gated string,
	resolution string,
) string {
	var message strings.Builder
	fmt.Fprintf(&message, "%s\n\n", subject)
	where := shortCommit(head)
	if branch != "" {
		where = branch + " at " + where
	}
	fmt.Fprintf(&message, "%s, onto main at %s:\n\n", where, shortCommit(mainCommit))
	merges := 0
	for _, commit := range commits {
		if commit.Merge {
			merges++
			continue
		}
		fmt.Fprintf(&message, "- %s %s\n", shortCommit(commit.Commit), commit.Subject)
	}
	if merges > 0 {
		fmt.Fprintf(&message, "- and %s of the branch's own\n", countedMerges(merges))
	}
	if resolution != "" {
		fmt.Fprintf(&message, "\n%s\n", resolution)
	}
	fmt.Fprintf(&message, "\n%s\n", gated)
	return message.String()
}

func countedMerges(count int) string {
	if count == 1 {
		return "1 merge"
	}
	return fmt.Sprintf("%d merges", count)
}

// mergeOntoMain is the tree a merge of head onto main makes, read without touching any working tree. A merge
// that conflicts is a *mergeConflict, naming its files and the tree git wrote with its markers.
func mergeOntoMain(worktree string, mainCommit string, head string) (string, error) {
	// --name-only: the tree, then one line per conflicted file, then a blank line and git's messages.
	output, gitCombinedError := gitCombined(worktree, "merge-tree", "--write-tree", "--name-only", mainCommit, head)
	lines := strings.Split(output, "\n")
	if gitCombinedError == nil && len(lines) > 0 && lines[0] != "" {
		return strings.TrimSpace(lines[0]), nil
	}
	conflicts := []string{}
	for _, line := range lines[min(1, len(lines)):] {
		if line == "" {
			break
		}
		if !slices.Contains(conflicts, line) {
			conflicts = append(conflicts, line)
		}
	}
	if len(conflicts) > 0 && len(lines) > 0 && lines[0] != "" {
		return "", &mergeConflict{Main: mainCommit, Tree: strings.TrimSpace(lines[0]), Paths: conflicts}
	}
	return "", fmt.Errorf(
		"merging this work onto main at %s: %w\n%s",
		shortCommit(mainCommit),
		gitCombinedError,
		output,
	)
}

// commitTree makes a commit of a tree with the parents given, first parent first, touching no ref and no
// working tree.
func commitTree(worktree string, tree string, message string, parents ...string) (string, error) {
	arguments := []string{"commit-tree", tree, "-m", message}
	for _, parent := range parents {
		arguments = append(arguments, "-p", parent)
	}
	commit, gitOutputError := gitOutput(worktree, arguments...)
	if gitOutputError != nil {
		return "", fmt.Errorf("making the landing commit: %w", gitOutputError)
	}
	return commit, nil
}
