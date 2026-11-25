package main

import (
	"bufio"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/system-inc/cohere/internal/format/format_files"
	"github.com/system-inc/cohere/internal/rule_names"
	"github.com/system-inc/cohere/internal/settings"
	"github.com/system-inc/cohere/internal/text_rules/file_name_words"
	"github.com/system-inc/cohere/internal/typescript_language/ecmascript/nextjs"
)

// A folder's name spells its words out (#zyq63kk). Kirk, Nov 13: "has a folder called docs, i want it to be
// documentation and have a rule on that." A folder holds no file a rule could report in when it holds only Markdown,
// as a docs/ does, so no language's rules could see it, and the check sits in the one walk that reads every directory a
// repository keeps: discovery's. One check, for TypeScript, Go and Swift projects alike, reported at the folder.
//
// The judgment is internal/text_rules/file_name_words' FolderAbbreviated: the folder's words, cut as a file stem's are,
// judged by policy/NameWords.json alone, with a registered rule's name set aside and a brand read across the folder and
// its parent, so google/docs is Google Docs. Never by the shared vocabulary, whose code meanings read a folder's plain
// words wrongly (Op art, a stage prop): a word judges folders by joining NameWords on its own census. `utils` and
// run-together words are other rules': nexus/consistency-no-utils-folder and
// nexus/consistency-require-separated-folder-words.
//
// Which folders are judged is the walk's own gaps: nothing .gitignore ignores, no node_modules, .build, .cache, .git
// or testdata, and no nested repository or submodule, none of which the walk enters, and beside those no folder the
// root's ignorePatterns take whole, none at or below a folder whose files are someone else's (vendor, Vendor,
// third_party), and no App Router folder, whose punctuation Next.js reads. The root's own name is the checkout's,
// which nobody inside can rename. Only the outermost offending folder is reported, once: renaming it is the moment
// to name what is inside it, and a child of a folder being renamed moves with it.
//
// It is keyed as configuration.AbbreviatedFolderName in the root's settings, the chain's top-level setting, so a
// project turns it off with a reason in its own file. A folder holds no comment, so there is no suppression: the
// folder is renamed, or the check is off for the project. It runs only on a run that lints.
//
// A finding has a path and no line, counts as a finding in the footer and fails the run, and prints as a `finding`
// record under `--json` with its own summary, labelled folderCheckLabel. A run with one goes the way a root of
// several projects does, its project checked in its own process and the folders reported by this one, so a project's
// recorded run never holds a folder verdict: each run judges the folders it walked, and a renamed folder is never
// replayed over.

// folderCheckLabel labels the folder check's summary under `--json`, beside each project's, as the format project at
// the root is labelled `. (Formats)`: no directory's label can be it.
const folderCheckLabel = ". (folders)"

// folderFinding is one folder whose name abbreviates a word the house spells out.
type folderFinding struct {
	// Directory is the folder, relative to the discovery root and slash-separated.
	Directory string
	MessageId string
	Message   string
}

// folderReasoning is the sentence both folder messages end on.
const folderReasoning = "A folder's name is read on every path and every import below it, so the letters saved when " +
	"it was named are paid back by every reader who has to expand the word themselves and hope they expanded it the " +
	"way the author meant."

// folderRuleNames are every language's rule names, from internal/rule_names, as cohere-go's file-name rule reads
// them, so a folder named for a rule it holds, internal/go_language/rules/no_param_reassign, is not judged on the
// rule's words.
var folderRuleNames = sync.OnceValue(func() file_name_words.RuleNames {
	return file_name_words.NewRuleNames(rule_names.RuleNames())
})

// isForeignToFolders reports whether a folder, and every folder below it, is someone else's: the file-name rules'
// foreign folders and the format walk's Vendor, as cohere-swift reads it.
func isForeignToFolders(name string) bool {
	return file_name_words.IsForeignFolder(name) || name == format_files.VendoredLayer
}

// judgeFolder is the finding for one directory the walk entered, relative to the root, and false when its name
// passes or is not the project's to judge. takenWhole reports whether the root's ignorePatterns take a directory
// whole.
func judgeFolder(relative string, takenWhole func(string) bool) (folderFinding, bool) {
	if relative == "." || takenWhole(relative) {
		return folderFinding{}, false
	}
	segments := strings.Split(relative, "/")
	if slices.ContainsFunc(segments, isForeignToFolders) {
		return folderFinding{}, false
	}
	name := segments[len(segments)-1]
	if nextjs.IsRoutingFolder(name) {
		return folderFinding{}, false
	}
	parent := ""
	if len(segments) > 1 {
		parent = segments[len(segments)-2]
	}
	finding, found := file_name_words.FolderAbbreviated(parent, name, folderRuleNames())
	if !found {
		return folderFinding{}, false
	}
	id, values := file_name_words.FolderAdvice(name, finding.Entry)
	// The message names what the word stands for, never the word twice (#13fpvdf): a folder named the word alone,
	// docs/, abbreviates "documentation", and one holding other words, api-docs/, abbreviates it as "docs".
	whole := strings.EqualFold(values["folder"], values["word"])
	expansions := make([]string, len(finding.Entry.Expansions))
	for index, expansion := range finding.Entry.Expansions {
		expansions[index] = strconv.Quote(expansion)
	}
	opening := fmt.Sprintf("Folder name %q abbreviates %s", values["folder"], strings.Join(expansions, " or "))
	if !whole {
		opening += fmt.Sprintf(" as %q", values["word"])
	}
	if len(expansions) > 1 {
		opening += ", whichever it means"
	}
	message := opening + ". " + folderReasoning
	if id == file_name_words.AbbreviatedFolderNameAdvice {
		// A folder named the word alone, vars/, would read `"vars" abbreviates "vars"`, so it says what the name is;
		// one holding other words, env-vars/, still names which word asks for the advice (#xd2q11d).
		opening = fmt.Sprintf("Folder name %q abbreviates %q.", values["folder"], values["word"])
		if whole {
			opening = fmt.Sprintf("Folder name %q is an abbreviation.", values["folder"])
		}
		message = fmt.Sprintf("%s %s %s", opening, values["advice"], folderReasoning)
	}
	return folderFinding{Directory: relative, MessageId: id, Message: message}, true
}

// keptFolders are the findings whose folder holds, at any depth the walk entered, a file the run covers: tracked, or
// untracked and not ignored (#13fpvdf). A folder whose every file is ignored, as a docs/ left holding only Finder's
// .DS_Store after git removed its tracked files at a rename, is no part of the project: no run checks a file in it,
// git does not know it, and no reader meets it on a path or an import. Nor is a folder with no file at all. covered
// is every directory the walk entered holding such a file itself, relative to the root.
func keptFolders(findings []folderFinding, covered map[string]bool) []folderFinding {
	kept := findings[:0:0]
	for _, finding := range findings {
		if covered[finding.Directory] {
			kept = append(kept, finding)
			continue
		}
		for directory := range covered {
			if isBelow(directory, finding.Directory) {
				kept = append(kept, finding)
				break
			}
		}
	}
	return kept
}

// outermostFolders are the findings with every one below another left out, sorted by path, so each offending tree is
// reported once, at its top.
func outermostFolders(findings []folderFinding) []folderFinding {
	slices.SortFunc(findings, func(left folderFinding, right folderFinding) int {
		return strings.Compare(left.Directory, right.Directory)
	})
	outermost := findings[:0:0]
	for _, finding := range findings {
		below := slices.ContainsFunc(outermost, func(above folderFinding) bool {
			return isBelow(finding.Directory, above.Directory)
		})
		if !below {
			outermost = append(outermost, finding)
		}
	}
	return outermost
}

// folderCheckSeverity is the folder check's setting in the root's settings, or in the lint config the command line
// named: the chain's top-level one, under zero config the house stack's, and off where it is unconfigured.
//
// A chain that does not load is the run's failure, named, never an off: a Go or Swift project's run never loads the
// TypeScript chain, so a root of only those, its settings refused and a docs/ in it, read green (#zyq63kk, proven
// on a scratch Go module before this failed closed).
func folderCheckSeverity(root string, lintConfigFileName string) (settings.Severity, error) {
	settingsPath := lintConfigFileName
	if settingsPath == "" {
		settingsPath = filepath.Join(root, "CohereSettings.json")
	}
	var loaded *settings.Configuration
	var loadError error
	if zero, isZeroConfigurationError := settings.IsZeroConfiguration(
		settingsPath,
	); isZeroConfigurationError == nil && zero {
		// The per-file sets' evidence is the program's, which discovery does not build, and a folder is no file, so
		// the stack is read without it: every set reaches cohere:style, where the check is ruled on.
		loaded, loadError = settings.LoadHouse(settingsPath, registeredRuleNames(), settings.HouseDetection{})
	} else {
		loaded, loadError = settings.LoadFor(settingsPath, registeredRuleNames())
	}
	if loadError != nil {
		return settings.SeverityOff, fmt.Errorf(
			"reading %s for the folder check, %s, which judges the folders this run walked: %w",
			settingsPath,
			settings.AbbreviatedFolderName,
			loadError,
		)
	}
	setting, configured := loaded.Rules[settings.AbbreviatedFolderName]
	if !configured {
		return settings.SeverityOff, nil
	}
	return setting.Severity, nil
}

// folderCheckSkipped is why this run's folder check judged no folder though its project turns the check on, for the
// summary's gaps (runGaps.FolderCheckSkipped), and empty when the check ran or is off.
var folderCheckSkipped string

// namesOnlyTheRoot reports whether a run's path arguments are its project's root and nothing else, each resolving to
// it, `.`, `./`, the root's absolute path or a symbolic link to it, read from base as the run reads its paths. Such a
// run covers the tree a bare run does, so it judges the same folders (#xd2q11d).
func namesOnlyTheRoot(arguments []string, base string, root string) bool {
	if len(arguments) == 0 {
		return false
	}
	resolvedRoot := resolvedPath(root)
	for _, argument := range arguments {
		if resolvedPath(absoluteFrom(base, argument)) != resolvedRoot {
			return false
		}
	}
	return true
}

// noteFolderCheckSkipped records, for the summary's gaps, that a run naming paths below its project's root skips
// the folder check, when its project turns the check on (#xd2q11d). A chain that does not load fails the run, as
// the check fails closed everywhere else.
func noteFolderCheckSkipped(location projectLocation) error {
	severity, folderCheckSeverityError := folderCheckSeverity(location.Root, location.LintConfigFileName)
	if folderCheckSeverityError != nil {
		return folderCheckSeverityError
	}
	if severity != settings.SeverityOff {
		folderCheckSkipped = "the run named paths below its project's root, and the folder check judges a whole " +
			"project's folders: run with no path, or the root as its path, to judge them"
	}
	return nil
}

// runLints reports whether a run given these flags runs the lint rules, which the folder check is one of: every run
// but one that only formats or only type-checks.
func runLints(given map[string]bool) bool {
	return !given["format-only"] && (given["lint"] || !given["types"])
}

// folderFindingRecord is one folder finding's `--json` line: a `finding` with a path and no line or column, since a
// folder has none.
type folderFindingRecord struct {
	Kind      string `json:"kind"`
	Path      string `json:"path"`
	Severity  string `json:"severity"`
	Rule      string `json:"rule"`
	MessageId string `json:"messageId"`
	Message   string `json:"message"`
}

// printFolderFindings prints each folder finding in the run's view, at the folder's absolute path, and under `--json`
// the folder check's summary after them, so a reader counting the summaries' verdicts reads the fail too.
func printFolderFindings(out *bufio.Writer, found discovery) {
	if len(found.Folders) == 0 {
		return
	}
	severity := found.FolderSeverity.String()
	for _, finding := range found.Folders {
		folder := filepath.Join(found.Root, filepath.FromSlash(finding.Directory))
		switch activeOutput.Mode {
		case outputJson:
			writeJsonLine(out, folderFindingRecord{
				Kind:      "finding",
				Path:      folder,
				Severity:  severity,
				Rule:      settings.AbbreviatedFolderName,
				MessageId: finding.MessageId,
				Message:   finding.Message,
			})
		case outputVerbose:
			fmt.Fprintf(out, "%s/ - %s %s: %s\n", folder, severity, settings.AbbreviatedFolderName,
				finding.Message)
		default:
			styled := activeOutput.Style.dim(severity)
			if found.FolderSeverity == settings.SeverityError {
				styled = activeOutput.Style.red(severity)
			}
			fmt.Fprintf(out, "%s/ %s %s %s\n", folder, styled,
				activeOutput.Style.dim(settings.AbbreviatedFolderName), finding.Message)
		}
	}
	if activeOutput.Mode == outputJson {
		writeJsonLine(out, summaryAsJson(folderSummaryOf(found)))
	}
}

// folderSummaryOf is the folder check as a run's summary: one rule, its findings, and a warning finding for each when
// the check is set to warn, so it fails only when the setting is an error.
func folderSummaryOf(found discovery) runSummary {
	summary := runSummary{Label: folderCheckLabel, Rules: 1, Findings: len(found.Folders)}
	if found.FolderSeverity == settings.SeverityWarn {
		summary.WarningFindings = summary.Findings
	}
	return summary
}
