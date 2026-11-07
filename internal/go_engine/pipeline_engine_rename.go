package go_engine

import (
	"cmp"
	"fmt"
	"go/token"
	"go/types"
	"maps"
	"slices"
	"strings"
	"time"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/check"
	"github.com/system-inc/cohere/internal/go_engine/contract"
	"github.com/system-inc/cohere/internal/go_engine/format"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_abbreviated_identifier"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_long_line"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_uppercase_initialism"
	"github.com/system-inc/cohere/internal/go_rename/global_rename"
	"github.com/system-inc/cohere/internal/go_rename/rename"
	"github.com/system-inc/cohere/internal/go_rename/workspace"
)

// renamingRules are the rules whose findings ask for a rename the engine applies after the load: each attaches one
// suggestion to a finding whose judgment names exactly one new spelling (see rule.RenameFixes), in this order, which
// is the order a name both of them ask to rename takes their renames in.
var renamingRules = []string{consistency_no_abbreviated_identifier.Name, consistency_no_uppercase_initialism.Name}

// renameRounds is how many rounds of renames one run makes, and how many proofs one round tries. A name can need a
// second round, as maxLen becomes maximumLen and only then is Len a finding, and a proof that fails drops the renames
// it named and tries the rest again.
const renameRounds = 3

// The reasons a rename is refused, the fix record's refusalsByReason keys. Each leaves the finding standing, and a
// note on stderr names the rename and the whole reason.
const (
	refusedUnwritable    = "a reference in a file this run can't write"
	refusedExported      = "an exported name in an importable package, which renameExports allows"
	refusedPlatform      = "a package it reaches has files this platform doesn't build"
	refusedUnproven      = "not proven by a load of the rewrite"
	refusedCgo           = "a project with cgo of its own, whose rewrite this run can't load"
	refusedNotDeclared   = "no declaration where the finding stands"
	refusedByRenameCheck = "unsafe to rename"
)

// nameRename is one rename a naming rule's finding asks for: the identifier the finding stands on, which declares the
// name, where the rule's one edit says it is, and the name the edit's text is.
type nameRename struct {
	rule    string
	file    string
	offset  int
	end     int
	line    int
	column  int
	newName string
}

// renameOf is the rename a finding asks for, recognized by its rule and the shape of its fix rather than its words:
// a naming rule's one suggestion, one edit in the finding's own file exactly over the identifier the finding stands
// on, whose text is a whole identifier.
func renameOf(found check.Finding) (nameRename, bool) {
	if !slices.Contains(renamingRules, found.Rule) || len(found.Suggestions) != 1 ||
		len(found.Suggestions[0].Edits) != 1 {
		return nameRename{}, false
	}
	edit := found.Suggestions[0].Edits[0]
	if edit.File != found.File || found.EndLine != found.Line || edit.End-edit.Start != found.EndColumn-found.Column ||
		!token.IsIdentifier(edit.Text) {
		return nameRename{}, false
	}
	return nameRename{
		rule: found.Rule, file: found.File, offset: edit.Start, end: edit.End, line: found.Line, column: found.Column,
		newName: edit.Text,
	}, true
}

// renamingRulesOn are the naming rules the registry holds and the settings leave on, by name.
func (engine *pipeline) renamingRulesOn() map[string]rule.Rule {
	on := map[string]rule.Rule{}
	for _, registered := range engine.registered {
		_, enabled := engine.severityOf(registered.Name)
		if enabled && slices.Contains(renamingRules, registered.Name) {
			on[registered.Name] = registered
		}
	}
	return on
}

// renamesNames is whether the run renames what the naming rules ask: only a run that writes and lints, so neither
// `--no-fix` nor `--format-only` renames, and only while the registry holds a naming rule and the settings leave it on.
// `--no-format` still renames: it leaves formatting out, and a rename is a rule's fix, as its help says. A file the
// rename rewrites is still printed by gofmt, since a longer name moves gofmt's alignment and leaving it would trade
// one finding for another, but the long-line wrap, which is formatting, does not run (wrapsLongLines).
func (engine *pipeline) renamesNames() bool {
	if engine.fix == nil || !engine.request.requests(contract.PhaseLint) {
		return false
	}
	for _, mode := range []string{"no-fix", "format-only"} {
		if engine.request.switches[mode] {
			return false
		}
	}
	return len(engine.renamingRulesOn()) > 0
}

// runRename is the part of the fix phase that needs types: the renames the naming rules ask for. The lint phase's rules
// run over the load first, through the findings cache, and the run keeps what they found for the lint phase, so a
// tree whose findings ask for no rename pays nothing past its one load. Otherwise it renames (see rename) and returns
// the load that proved what it wrote, which the types and lint phases read in place of the first, with no lint pass,
// and marks the run renamed, so the lint phase runs without the findings cache: the first pass's findings and the
// cache's keys are of the bytes it replaced.
func (engine *pipeline) runRename(loaded *load.Loaded, loadError error) (*load.Loaded, *lintPass) {
	if !engine.renamesNames() || loadError != nil || len(loaded.Diagnostics) > 0 {
		return loaded, nil
	}
	started := time.Now()
	first := engine.lint(loaded)
	if first.checkError != nil {
		return loaded, &first
	}
	asked := engine.askedRenames(first.outcome.kept)
	if len(asked) == 0 {
		return loaded, &first
	}
	if loaded.CgoTranslated {
		for _, refused := range asked {
			engine.refuseRename(refused, engine.spelledAt(refused), refusedCgo,
				"the cgo tool reads its files from disk, so the rewrite cannot be loaded before it is written")
		}
		return loaded, &first
	}
	written := engine.rename(asked)
	if written == nil {
		// Nothing was written, so the first load and its lint pass are still of the tree on disk. What this cost was
		// the fix phase's, but the lint pass, which the lint phase reports.
		engine.fix.elapsed += time.Since(started) - first.elapsed
		return loaded, &first
	}
	engine.renamed = true
	// The first load and its lint pass served only the rename, and the load that proved it is the types phase's.
	engine.fix.elapsed += loaded.Elapsed + time.Since(started) - written.Elapsed
	return written, nil
}

// askedRenames are the renames the kept findings ask for, in the order of renamingRules and then of the findings.
func (engine *pipeline) askedRenames(kept []check.Finding) []nameRename {
	var asked []nameRename
	for _, found := range kept {
		if renamed, renames := renameOf(found); renames {
			asked = append(asked, renamed)
		}
	}
	slices.SortStableFunc(asked, func(left nameRename, right nameRename) int {
		return cmp.Compare(slices.Index(renamingRules, left.rule), slices.Index(renamingRules, right.rule))
	})
	return asked
}

// renameRound is what one round of renames proved: the load of the rewrite, the rewritten files' bytes over every
// round's so far, and for each file the identifiers renamed, under the rule that asked, and the lines its wrap cured.
type renameRound struct {
	proof   *load.Loaded
	overlay map[string][]byte
	fixedBy map[string]map[string]int
	wrapped map[string]int

	// declined is why the wrap was declined in each file it declined, which holds gofmt's output of the rename.
	declined map[string]string

	// applied are the renames asked for that the proven plan made, each refused as unwritable if the set is not.
	applied []askedProposal
}

// rename renames what asked names, in rounds of at most renameRounds, and writes the result: the load of the tree it
// wrote, or nil when it wrote nothing. A rename needs syntax and the findings cache restores none, so the tree is
// loaded again from source first. Each round plans over the last load, rewrites in memory, formats each file as the
// fix phase does, and proves the rewrite with a load reading those bytes, before anything is written (see
// renameOnce). After a proven round the naming rules run over its load, which is from source and holds the rewrite,
// and what they ask next is the next round, so a name that needs two renames gets both. Every round's bytes are
// written together, once, as one set (see writeRenamed), and the last round's proof is the load of what is on disk.
func (engine *pipeline) rename(firstAsked []nameRename) *load.Loaded {
	current, loadError := load.Packages(engine.described.workspace.Root, engine.described.workspace.Patterns(),
		engine.described.workspace.Environment(), engine.described.readableOurs)
	if loadError == nil && len(current.Diagnostics) > 0 {
		loadError = fmt.Errorf("it found %d errors the first load did not", len(current.Diagnostics))
	}
	if loadError != nil {
		for _, refused := range firstAsked {
			engine.refuseRename(refused, engine.spelledAt(refused), refusedUnproven,
				"the load to plan it over failed: "+oneLine(loadError))
		}
		return nil
	}
	asked := firstAsked
	overlay := map[string][]byte{}
	fixedBy := map[string]map[string]int{}
	wrapped := map[string]int{}
	var proven *load.Loaded
	// applied are the renames the proven rounds made, a name renamed in two rounds once, under its first rename: it is
	// one finding, which a set that cannot be written leaves standing.
	var applied []askedProposal
	renamedTo := map[[2]string]bool{}
	declined := map[string]string{}
	for round := 1; round <= renameRounds && len(asked) > 0; round++ {
		result := engine.renameOnce(current, overlay, asked)
		if result.proof == nil {
			break
		}
		for _, renamed := range result.applied {
			if !renamedTo[[2]string{renamed.rename.file, renamed.oldName}] {
				applied = append(applied, renamed)
			}
			renamedTo[[2]string{renamed.rename.file, renamed.rename.newName}] = true
		}
		proven, current, overlay = result.proof, result.proof, result.overlay
		for file, byRule := range result.fixedBy {
			if fixedBy[file] == nil {
				fixedBy[file] = map[string]int{}
			}
			for name, count := range byRule {
				fixedBy[file][name] += count
			}
		}
		for file, lines := range result.wrapped {
			wrapped[file] += lines
		}
		// A later round that rewrites a file wraps it again, and its word is the one the written bytes hold. In a run
		// that wraps, every file a round rewrote has its count in the round's wrapped, none or more.
		for file := range result.wrapped {
			delete(declined, file)
		}
		maps.Copy(declined, result.declined)
		if round < renameRounds {
			asked = engine.askedOver(current, overlay)
		}
	}
	if proven == nil || !engine.writeRenamed(overlay, fixedBy, wrapped, applied) {
		return nil
	}
	for _, file := range slices.Sorted(maps.Keys(declined)) {
		// Not a gap, as the wrap after the load says of its own: the file holds gofmt's output of the rename, and the
		// lint phase reports every long line in it.
		engine.note("%s: its long lines were not wrapped, so they stay findings: %s", file, declined[file])
	}
	return proven
}

// writeRenamed writes every renamed file's proven bytes as one set (format.WriteAllAtomically), since a rename's files
// compile only together, and reads each one again, so the //cohere:ignore comments the phases after it read are the
// lines on disk. When the set is written it adds the files to the fix record: once each, merged with the entry the fix
// phase made for a file gofmt or the wrap wrote, the identifiers renamed counted under the rule that asked for them
// and the lines the wrap cured under the long-line rule. When it is not, nothing was written, every rename applied is
// refused as unwritable and its finding stands, and it returns false. A set it could not put back is a gap, named.
func (engine *pipeline) writeRenamed(
	overlay map[string][]byte, fixedBy map[string]map[string]int, wrapped map[string]int, applied []askedProposal,
) bool {
	partlyWritten, writeError := format.WriteAllAtomically(overlay)
	for _, file := range slices.Sorted(maps.Keys(overlay)) {
		engine.reread(file)
	}
	if writeError != nil {
		for _, renamed := range applied {
			engine.refuseRename(renamed.rename, renamed.oldName, refusedUnwritable, "the files it rewrites could not "+
				"all be written, so none was: "+oneLine(writeError))
		}
		if len(partlyWritten) > 0 {
			engine.incomplete = true
			engine.note("%s hold a rename and could not be put back after the rest of its files failed to write, so "+
				"the tree on disk is half renamed", strings.Join(partlyWritten, ", "))
		}
		return false
	}
	for _, file := range slices.Sorted(maps.Keys(overlay)) {
		counts := maps.Clone(fixedBy[file])
		if counts == nil {
			counts = map[string]int{}
		}
		if wrapped[file] > 0 {
			counts[consistency_no_long_line.Name] += wrapped[file]
		}
		for _, count := range counts {
			engine.fix.record.FixesApplied += count
		}
		index := slices.IndexFunc(engine.fix.record.ChangedFiles, func(changed contract.ChangedFile) bool {
			return changed.File == file
		})
		if index < 0 {
			engine.fix.record.FilesRewritten++
			engine.fix.record.ChangedFiles = append(engine.fix.record.ChangedFiles,
				contract.ChangedFile{File: file, FixedBy: counts, Formatted: false})
			continue
		}
		for name, count := range counts {
			engine.fix.record.ChangedFiles[index].FixedBy[name] += count
		}
	}
	// In the order of the files' names, as gofmt's and the wrap's entries are.
	slices.SortStableFunc(engine.fix.record.ChangedFiles, func(left, right contract.ChangedFile) int {
		return cmp.Compare(left.File, right.File)
	})
	return true
}

// renameOnce is one round: the renames asked for over current, the last load, whose rewritten files' bytes are in
// overlay. It plans them, rewrites and formats every file they touch in memory, and proves the rewrite with a load
// from source reading those bytes: zero errors, and every identifier resolving to the declaration it did before,
// under its expected name (global_rename.Compare). A proof that fails names the renames it caught, which are dropped
// and the rest planned and proven again, at most renameRounds times. Every rename asked for is accounted once: in
// the proven rewrite, or refused with its reason. Its proof is nil when nothing was proven.
func (engine *pipeline) renameOnce(current *load.Loaded, overlay map[string][]byte, asked []nameRename) renameRound {
	writable := func(filename string) bool { return engine.described.inScopeSet[filename] }
	space := workspace.FromLoaded(engine.described.workspace.Root, current.Roots, current.Packages, writable)
	proposals, askedBy := engine.proposals(current, asked)
	// Types can't prove a name nothing spells but a string: an encoder reading a field by its name, a template or
	// reflect calling a member by it, a literal spelling it. So an untagged encoded field is declined rather than given
	// a tag that keeps the old key (TagEncodedFields), and the string-literal guard reviews no literal away.
	options := global_rename.Options{
		RenameImplementations: true,
		HostOnly:              true,
		TagEncodedFields:      false,
		ReviewedLiterals:      nil,
		PinnedLibraries:       map[string]string{},
	}
	for _, on := range engine.renamingRulesOn() {
		if !rule.RenamesExports(on.Analyzer) {
			continue
		}
		// A rule's renameExports says the workspace is the whole world. proposals already refused the exported names
		// of a rule without it, so pinning reaches the rest: an implementation or embedded field renamed with them.
		for _, root := range current.Roots {
			options.PinnedLibraries[strings.TrimSuffix(root.PkgPath, "_test")] = "renameExports is set"
		}
	}
	// unproven are the renames a failed proof named, by old and new name, with why. Each is dropped before the next
	// try, so a rename is unproven or in the last plan, never both.
	unproven := map[[2]string]string{}
	var proven renameRound
	var last *global_rename.Result
	for attempt := 1; attempt <= renameRounds && len(proposals) > 0; attempt++ {
		last = global_rename.Plan(space, proposals, options)
		if len(last.Accepted) == 0 {
			break
		}
		proved := engine.prove(space, overlay, last)
		failures := proved.failures
		if len(failures) == 0 {
			proven = renameRound{
				proof: proved.proof, overlay: proved.overlay, fixedBy: renamedCounts(last, askedBy),
				wrapped: proved.wrapped, declined: proved.declined, applied: acceptedAsked(last, askedBy),
			}
			break
		}
		maps.Copy(unproven, failures)
		before := len(proposals)
		proposals = slices.DeleteFunc(proposals, func(proposal global_rename.Proposal) bool {
			_, failed := failures[[2]string{proposal.Object.Name(), proposal.NewName}]
			return failed
		})
		if len(proposals) < before {
			continue
		}
		// The failures name no rename asked for, so the same plan would fail the same way: none of it is proven.
		for _, accepted := range last.Accepted {
			unproven[[2]string{accepted.OldName, accepted.NewName}] = "the proof failed on what no rename asked for " +
				"explains: " + slices.Sorted(maps.Values(failures))[0]
		}
		break
	}
	if last != nil {
		engine.refuseRejected(last, askedBy)
	}
	for _, asked := range askedBy {
		if reason, failed := unproven[[2]string{asked.oldName, asked.rename.newName}]; failed {
			engine.refuseRename(asked.rename, asked.oldName, refusedUnproven, reason)
		}
	}
	return proven
}

// askedProposal is a rename asked for, as the plan is given it, with the name it renames.
type askedProposal struct {
	rename  nameRename
	oldName string
}

// proposals are the renames asked for as the plan takes them: each finding's edit position is the identifier that
// declares the name in current, by file and byte offset. A rename whose finding stands on no declaration, or that
// renames an exported name of an importable package for a rule without renameExports, is refused here; two renames of
// one declaration keep the first, and the second is asked again by the next round if it still stands.
func (engine *pipeline) proposals(
	current *load.Loaded, asked []nameRename,
) ([]global_rename.Proposal, []askedProposal) {
	type declared struct {
		owner  *packages.Package
		object types.Object
	}
	declarations := map[rename.EditKey]declared{}
	for _, analyzed := range current.Packages {
		for identifier, object := range analyzed.TypesInfo.Defs {
			if object == nil {
				continue
			}
			position := analyzed.Fset.PositionFor(identifier.Pos(), false)
			key := rename.EditKey{Filename: position.Filename, Offset: position.Offset}
			if _, held := declarations[key]; !held {
				declarations[key] = declared{owner: analyzed, object: object}
			}
		}
	}
	rules := engine.renamingRulesOn()
	var proposals []global_rename.Proposal
	var askedBy []askedProposal
	proposed := map[types.Object]bool{}
	for _, each := range asked {
		// Keyed by the name its finding stands on, as the bytes this round reads spell it, which needs no declaration:
		// a rename refused in an earlier round whose finding still stands is not asked for, or counted, again.
		spelled := engine.spelledAt(each)
		if engine.refusedRenames[[3]string{each.file, spelled, each.newName}] {
			continue
		}
		found, held := declarations[rename.EditKey{Filename: each.file, Offset: each.offset}]
		if !held {
			engine.refuseRename(each, spelled, refusedNotDeclared, "no identifier declares a name at its position")
			continue
		}
		if proposed[found.object] {
			continue
		}
		if exportedFromImportable(found.owner, found.object, each.file) &&
			!rule.RenamesExports(rules[each.rule].Analyzer) {
			engine.refuseRename(each, found.object.Name(), refusedExported, fmt.Sprintf("%s is importable, so code "+
				"outside the workspace may name it; set renameExports on %s when the workspace is the whole world",
				found.owner.PkgPath, each.rule))
			continue
		}
		proposed[found.object] = true
		proposals = append(proposals,
			global_rename.Proposal{Package: found.owner, Object: found.object, NewName: each.newName})
		askedBy = append(askedBy, askedProposal{rename: each, oldName: found.object.Name()})
	}
	return proposals, askedBy
}

// spelledAt is the name a rename's finding stands on, as the bytes the run holds for its file spell it: the file as
// read, or as the last proven round rewrote it (see askedOver). Empty when the run holds no such bytes.
func (engine *pipeline) spelledAt(asked nameRename) string {
	contents, held := engine.described.contents[asked.file]
	if !held || asked.offset < 0 || asked.end > len(contents) || asked.offset > asked.end {
		return ""
	}
	return string(contents[asked.offset:asked.end])
}

// exportedFromImportable is whether object is a name code outside the workspace may read: exported, declared where
// another package can name it (at package level, or a field or method), in a package another module could import,
// one under no `internal` directory that is not a command, and not in a test.
func exportedFromImportable(owner *packages.Package, object types.Object, file string) bool {
	if !token.IsExported(object.Name()) {
		return false
	}
	member := false
	switch typed := object.(type) {
	case *types.Var:
		member = typed.IsField()
	case *types.Func:
		member = typed.Signature().Recv() != nil
	}
	if !member && (object.Pkg() == nil || object.Parent() != object.Pkg().Scope()) {
		return false
	}
	if owner.Name == "main" || strings.HasSuffix(owner.Name, "_test") || strings.HasSuffix(file, "_test.go") {
		return false
	}
	return !slices.Contains(strings.Split(owner.PkgPath, "/"), "internal")
}

// rewriteProof is what prove made of a plan: the load of the rewrite, every rewritten file's bytes over the overlay's,
// the lines the wrap cured in each, why the wrap was declined in each it declined, and the renames the proof failed,
// by old and new name with why, none when it held. Its load is nil when the rewrite or its load failed.
type rewriteProof struct {
	proof    *load.Loaded
	overlay  map[string][]byte
	wrapped  map[string]int
	declined map[string]string
	failures map[[2]string]string
}

// prove rewrites result's files over overlay, wraps each as the fix phase does, and loads the workspace from source
// reading them (see rewriteProof).
func (engine *pipeline) prove(
	space *workspace.Workspace, overlay map[string][]byte, result *global_rename.Result,
) rewriteProof {
	everyRename := func(reason string) map[[2]string]string {
		failures := map[[2]string]string{}
		for _, accepted := range result.Accepted {
			failures[[2]string{accepted.OldName, accepted.NewName}] = reason
		}
		return failures
	}
	expectation := global_rename.Expect(space, result)
	rewritten, rewriteError := global_rename.RewrittenFilesOver(result, overlay)
	if rewriteError != nil {
		return rewriteProof{failures: everyRename("the rewrite failed: " + oneLine(rewriteError))}
	}
	// The rewrite is gofmt's already: the library formats each file it rewrites with go/format, as the fix phase's
	// gofmt does. A longer name can push a line past the limit, so in a run that wraps each file is wrapped through the
	// fix phase's own step and hook, with the long-file guard and every comment rule; a declined wrap keeps gofmt's
	// output, its long lines stay findings, and its reason is noted once the set is written.
	wraps := engine.wrapsLongLines()
	wrap := engine.longLineWrap()
	next := maps.Clone(overlay)
	wrapped := map[string]int{}
	declined := map[string]string{}
	for file, contents := range rewritten {
		next[file] = contents
		if !wraps {
			continue
		}
		var reason string
		next[file], wrapped[file], reason = format.WrapSource(file, contents, wrap)
		if reason != "" {
			declined[file] = reason
		}
	}
	proof, proofError := load.PackagesWithOverlay(engine.described.workspace.Root,
		engine.described.workspace.Patterns(), engine.described.workspace.Environment(), engine.described.readableOurs,
		next)
	if proofError != nil {
		return rewriteProof{failures: everyRename("the load of the rewrite failed: " + oneLine(proofError))}
	}
	failures := map[[2]string]string{}
	for _, diagnostic := range proof.Diagnostics {
		reason := fmt.Sprintf("the rewrite does not type-check: %s:%d: %s", diagnostic.File, diagnostic.Line,
			diagnostic.Message)
		keepFirst(failures, renamesIn(result, diagnostic.File, reason, everyRename))
	}
	proved := workspace.FromLoaded(engine.described.workspace.Root, proof.Roots, proof.Packages, nil)
	for _, mismatch := range global_rename.Compare(expectation, proved).Mismatches {
		reason := "the rewrite reads back otherwise: " + mismatch.String()
		switch mismatch.Kind {
		case global_rename.MismatchKindIdentifier:
			named := map[[2]string]string{}
			for _, accepted := range result.Accepted {
				names := []string{accepted.OldName, accepted.NewName}
				if slices.Contains(names, mismatch.Expected.Name) || slices.Contains(names, mismatch.Actual.Name) {
					named[[2]string{accepted.OldName, accepted.NewName}] = reason
				}
			}
			if len(named) == 0 {
				named = everyRename(reason)
			}
			keepFirst(failures, named)
		case global_rename.MismatchKindIdentifierCount:
			keepFirst(failures, renamesIn(result, mismatch.Filename, reason, everyRename))
		default:
			keepFirst(failures, everyRename(reason))
		}
	}
	return rewriteProof{proof: proof, overlay: next, wrapped: wrapped, declined: declined, failures: failures}
}

// keepFirst adds to failures each rename of more it does not hold yet, so a rename's reason is the first failure found:
// a type error before the mismatch it causes.
//
// Mutates: failures gains each rename of more it did not hold
func keepFirst(failures map[[2]string]string, more map[[2]string]string) {
	for renamed, reason := range more {
		if _, held := failures[renamed]; !held {
			failures[renamed] = reason
		}
	}
}

// renamesIn are the renames with an edit in file, by old and new name with reason, or every rename when none has: a
// failure in a file no rename touched can't be told from any of them.
func renamesIn(
	result *global_rename.Result, file string, reason string, everyRename func(string) map[[2]string]string,
) map[[2]string]string {
	named := map[[2]string]string{}
	for _, edit := range slices.Concat(result.Edits, result.TagEdits, result.CommentEdits, result.TwinEdits) {
		if edit.Filename == file {
			named[[2]string{edit.OldText, edit.NewText}] = reason
		}
	}
	if len(named) == 0 {
		return everyRename(reason)
	}
	return named
}

// renamedCounts is, for each file a proven plan rewrote, the identifiers it renamed there, each once however many
// variants renamed it, counted under the rule that asked for the rename. An implementation or an embedded field
// renamed with a name takes the same old and new name, and so the same rule. A comment respelling the name is not
// counted, and is always in a file whose identifier the plan renamed: the declaration's own.
func renamedCounts(result *global_rename.Result, askedBy []askedProposal) map[string]map[string]int {
	ruleOf := map[[2]string]string{}
	for _, asked := range askedBy {
		pair := [2]string{asked.oldName, asked.rename.newName}
		if _, held := ruleOf[pair]; !held {
			ruleOf[pair] = asked.rename.rule
		}
	}
	edits, _ := rename.Deduplicate(slices.Concat(result.Edits, result.TwinEdits))
	counts := map[string]map[string]int{}
	for _, edit := range edits {
		name, held := ruleOf[[2]string{edit.OldText, edit.NewText}]
		if !held {
			continue
		}
		if counts[edit.Filename] == nil {
			counts[edit.Filename] = map[string]int{}
		}
		counts[edit.Filename][name]++
	}
	return counts
}

// acceptedAsked are the renames asked for that the plan accepted, matched by where their declarations stand.
func acceptedAsked(result *global_rename.Result, askedBy []askedProposal) []askedProposal {
	var accepted []askedProposal
	for _, asked := range askedBy {
		if slices.ContainsFunc(result.Accepted, func(renamed global_rename.Rename) bool {
			return renamed.Position.Filename == asked.rename.file && renamed.Position.Offset == asked.rename.offset
		}) {
			accepted = append(accepted, asked)
		}
	}
	return accepted
}

// refuseRejected refuses each rename asked for that the plan rejected, matched by where its declaration stands, with
// the plan's reason, under the reason the fix record counts it by.
func (engine *pipeline) refuseRejected(result *global_rename.Result, askedBy []askedProposal) {
	for _, rejection := range result.Rejections {
		for _, asked := range askedBy {
			if rejection.Position.Filename != asked.rename.file || rejection.Position.Offset != asked.rename.offset {
				continue
			}
			reason := refusedByRenameCheck + " (" + rejection.Class + ")"
			switch rejection.Class {
			case "file":
				reason = refusedUnwritable
			case "platform":
				reason = refusedPlatform
			}
			engine.refuseRename(asked.rename, asked.oldName, reason, rejection.Reason)
		}
	}
}

// refuseRename counts one rename as refused in the fix record, under reason, and says on stderr which and why. oldName
// is the name it would have renamed, as its finding's span spells it (spelledAt), and keys the refusal, so a later
// round does not count it again; empty only when the run holds no bytes for its file.
func (engine *pipeline) refuseRename(refused nameRename, oldName string, reason string, detail string) {
	if engine.refusedRenames == nil {
		engine.refusedRenames = map[[3]string]bool{}
	}
	engine.refusedRenames[[3]string{refused.file, oldName, refused.newName}] = true
	engine.fix.record.FixesRefused++
	engine.fix.record.RefusalsByReason[reason]++
	what := "the name"
	if oldName != "" {
		what = oldName
	}
	engine.note("%s:%d:%d: %s was not renamed to %s, %s: %s", refused.file, refused.line, refused.column, what,
		refused.newName, reason, detail)
}

// askedOver are the renames the naming rules ask for over a proven round's load, whose rewritten files' bytes are in
// overlay: the two rules alone, without the findings cache, whose keys are of the bytes on disk, and the
// //cohere:ignore comments read from the rewrite. Neither rule learns facts, so no package below the analyzed ones is
// analyzed, and the load keeps the syntax the next plan and the lint phase read.
func (engine *pipeline) askedOver(current *load.Loaded, overlay map[string][]byte) []nameRename {
	on := engine.renamingRulesOn()
	naming := make([]rule.Rule, 0, len(on))
	names := make([]string, 0, len(on))
	for _, name := range slices.Sorted(maps.Keys(on)) {
		if len(on[name].Analyzer.FactTypes) > 0 {
			// A rule that learns facts would be analyzed on every dependency, which lets go of their syntax.
			engine.note("%s learns facts, so its renames are not asked for again after a round", name)
			return nil
		}
		naming = append(naming, on[name])
		names = append(names, name)
	}
	result, checkError := check.Run(current.Packages, naming, check.Options{
		SingleThreaded: engine.request.switches["single-threaded"],
	})
	if checkError != nil {
		engine.note("the naming rules could not run over the renamed tree, so no further round renames: %v",
			checkError)
		return nil
	}
	for file, contents := range overlay {
		engine.described.contents[file] = contents
	}
	return engine.askedRenames(engine.suppress(names, engine.inScope(result.Findings)).kept)
}
