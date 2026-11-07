package main

import (
	"bufio"
	"bytes"
	"fmt"
	"path/filepath"

	"github.com/system-inc/cohere/internal/format/format_files"
	"github.com/system-inc/cohere/internal/lint/configuration"
	"github.com/system-inc/cohere/internal/streams"
)

// Every ignore says what it kept out of the run (#cxeq6jw). Discovery reads the root's ignorePatterns whole and
// counts, for each entry, the paths it matched among every entry of every directory the walk read, before any
// ignore applied, inside repositories of their own too, and below the path an entry names where the walk did
// not go (see discovery.IgnoreMatched). `--verbose` lists each entry with its reason, kind, file and count, a
// count of zero included, then cohere's own defaults (see defaultIgnores), and `--json` carries the same list. A
// dead one is a finding (see unused_ignore.go).
//
// Discovery is the one place a whole-tree run always passes, and it runs fresh on every run, a replayed one
// included. So the account is this invocation's, never the run cache's: a run of one project says it at its
// end, beside the departures, as lines the recording leaves out (printInvocation), and adds it to the summary
// only after the summary is recorded, as the Swift no package holds is (see writeRunEnd and replayRunCache). A
// run of several says it before the projects' sections, being no project's. A run that names a directory, a
// file or a tsconfig does not discover, and says nothing of its ignores: it never considered the whole tree.

// discoveredIgnores are the root's ignorePatterns as this run's discovery counted them, then cohere's defaults,
// nil when the run did not discover.
var discoveredIgnores []ignoreAccount

// discoveredIgnoresWritten is whether this run has said them, which it does once (see writeDiscoveredIgnores).
var discoveredIgnoresWritten bool

// ignoreAccount is one ignorePatterns entry as a run reports it: the entry, the settings file or set that wrote
// it, and how many paths it matched this run.
type ignoreAccount struct {
	Pattern string `json:"pattern"`
	// Reason, Kind and Check are the entry's own, absent when it gave none.
	Reason string `json:"reason,omitempty"`
	Kind   string `json:"kind,omitempty"`
	Check  string `json:"check,omitempty"`
	// File is the settings file that wrote the entry, absolute, or the set's name, such as
	// cohere:system-inc/structure.
	File string `json:"file"`
	// Matched is how many paths the entry matched among those discovery considered: files, and directories it
	// takes whole.
	Matched int `json:"matched"`
	// Default is one of cohere's own ignores, which no settings file wrote and none can delete (see
	// defaultIgnores). Its File is "cohere".
	Default bool `json:"default,omitempty"`

	// shownFile is File as --verbose names it: relative to the root, or the set's name.
	shownFile string
}

// ignoreAccountsOf pairs each entry with what discovery counted for it, matched holding one count per entry in
// the entries' order, the order discovery was handed their globs in. root is the directory the globs are
// relative to.
func ignoreAccountsOf(entries []configuration.IgnoreEntry, matched []int, root string) []ignoreAccount {
	accounts := make([]ignoreAccount, 0, len(entries))
	for index, entry := range entries {
		account := ignoreAccount{
			Pattern:   entry.Pattern,
			Reason:    entry.Reason,
			Kind:      string(entry.Kind),
			Check:     string(entry.Check),
			File:      entry.File,
			shownFile: entry.File,
		}
		if !configuration.IsSet(entry.File) {
			if relative, relativePathError := filepath.Rel(root, entry.File); relativePathError == nil {
				account.shownFile = relative
			}
		}
		if index < len(matched) {
			account.Matched = matched[index]
		}
		accounts = append(accounts, account)
	}
	return accounts
}

// defaultIgnores are the trees cohere never enters, listed after the root's own entries so a reader sees each as
// an ignore like any other, with its kind and its reason (#cxeq6jw). Each holds only where its tree is already
// skipped: Go's testdata by the Go engine, which never enters one, and by the format walk and discovery, and
// Vendor by the format walk and the Swift engine. Neither is a glob any check matches a file against, so a file a
// tsconfig includes under either is type-checked and linted as any other, and neither is ever a dead ignore,
// since no settings file wrote it. Matched counts the directories by that name the walk's listings held.
var defaultIgnores = []struct {
	directory string
	kind      configuration.IgnoreKind
	reason    string
}{
	{
		format_files.FixturesLayer,
		configuration.IgnoreKindInput,
		"Go's test fixtures, read by their tests byte for byte: the Go engine and the format walk never enter " +
			"a testdata directory, and a file a tsconfig includes there is still type-checked and linted.",
	},
	{
		format_files.VendoredLayer,
		configuration.IgnoreKindThirdParty,
		"Somebody else's code, vendored whole: the format walk and the Swift engine never enter a Vendor " +
			"directory, and a file a tsconfig includes there is still type-checked and linted.",
	},
}

// isDefaultIgnore reports whether a directory's name is one of defaultIgnores.
func isDefaultIgnore(name string) bool {
	for _, ignore := range defaultIgnores {
		if ignore.directory == name {
			return true
		}
	}
	return false
}

// defaultIgnoreAccounts are defaultIgnores as a run reports them, matched holding discovery's count of each by
// directory name.
func defaultIgnoreAccounts(matched map[string]int) []ignoreAccount {
	accounts := make([]ignoreAccount, 0, len(defaultIgnores))
	for _, ignore := range defaultIgnores {
		accounts = append(accounts, ignoreAccount{
			Pattern:   "**/" + ignore.directory + "/**",
			Reason:    ignore.reason,
			Kind:      string(ignore.kind),
			File:      "cohere",
			Matched:   matched[ignore.directory],
			Default:   true,
			shownFile: "cohere's defaults",
		})
	}
	return accounts
}

// line is the entry under `--verbose`: its pattern, the file that wrote it, its kind and check when it gave
// them, how many paths it matched, and its reason, or that it gave none.
func (account ignoreAccount) line() string {
	described := fmt.Sprintf("%q in %s", account.Pattern, account.shownFile)
	if account.Kind != "" {
		described += ", " + account.Kind
	}
	if account.Check != "" {
		described += ", read by the " + account.Check + " check"
	}
	reason := account.Reason
	if reason == "" {
		reason = "(no reason)"
	}
	return fmt.Sprintf("  ignore: %s, matched %s: %s\n", described, counted(account.Matched, "path", "paths"), reason)
}

// writeDiscoveredIgnores says discovery's account, once: each dead ignore as a finding, in every view, and
// every entry under `--verbose`, as lines of this invocation's that a recording leaves out.
func writeDiscoveredIgnores(out *bufio.Writer) {
	if discoveredIgnoresWritten || len(discoveredIgnores) == 0 {
		return
	}
	discoveredIgnoresWritten = true
	var buffer bytes.Buffer
	rendered := bufio.NewWriter(&buffer)
	writeIgnoreAccounts(rendered, discoveredIgnores)
	streams.Show(rendered)
	printInvocation(out, "%s", buffer.String())
	streams.Show(out)
}

// writeIgnoreAccounts prints each dead ignore as a finding, in every view (see writeUnusedIgnores), and lists
// every entry under `--verbose`.
func writeIgnoreAccounts(out *bufio.Writer, accounts []ignoreAccount) {
	writeUnusedIgnores(out, accounts)
	account := accountOutput(out)
	for _, entry := range accounts {
		fmt.Fprint(account, entry.line())
	}
}
