package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"sort"
	"strings"

	"github.com/system-inc/cohere/internal/go_rename/rename"
	"github.com/system-inc/cohere/internal/naming/go_origin_names"

	"golang.org/x/tools/go/cfg"
)

// This file splits a local error written more than once into one variable per web: a definition and the
// reads only it reaches, joined with every other definition reaching one of those reads. Webs come from
// reaching definitions over the function's control-flow graph (golang.org/x/tools/go/cfg). Giving each web
// its own variable keeps every read's value, because the last write before a read always reaches it and so
// sits in its web; any grouping of whole webs into variables keeps it too, which is why the analysis may
// over-approximate reachability (merging more) but never under-approximate it. What can go wrong is only
// where each new variable is declared, which the per-package check and the final reload prove.

// functionUnit is one function body the analysis runs over: a declared function's or a literal's.
type functionUnit struct {
	node ast.Node
	body *ast.BlockStmt
}

func newFunctionIndex(files []*ast.File) *functionIndex {
	index := &functionIndex{graphs: map[*ast.BlockStmt]*cfg.CFG{}}
	for _, file := range files {
		ast.Inspect(file, func(node ast.Node) bool {
			switch typed := node.(type) {
			case *ast.FuncDecl:
				if typed.Body != nil {
					index.units = append(index.units, functionUnit{node: typed, body: typed.Body})
				}
			case *ast.FuncLit:
				index.units = append(index.units, functionUnit{node: typed, body: typed.Body})
			}
			return true
		})
	}
	return index
}

// definitionSite is one write of the variable: the identifier written and the statement writing it.
type definitionSite struct {
	identifier *ast.Ident
	// statement is the *ast.AssignStmt, *ast.ValueSpec or *ast.RangeStmt writing it.
	statement ast.Node
	// index is the identifier's position on the statement's left side.
	index int
	// parameter marks a parameter's own declaration, written by the caller before the body runs.
	parameter bool
}

// group is one variable of the split: the original, kept at its declaration, or a new one declared at its
// anchor, the earliest definition of its webs.
type group struct {
	newName string
	webs    []*web
	// host marks the original variable.
	host bool
	// anchor, for a new variable, is its declaring definition and scope its scope.
	anchor definitionSite
	scope  *types.Scope
	// visibleFrom is where a new variable's scope begins: the end of its anchor's statement, or for one
	// declared by an inserted `var`, the start of the statement it is inserted before.
	visibleFrom token.Pos
	// insertion, for a variable declared by an inserted `var`, says where, and indent how far in.
	insertion *rename.Insertion
	indent    string
	// anchorOffset is the anchor identifier's offset in its file, for the report.
	anchorOffset int
}

// splitPlan is the result of splitting one variable: its edits, the identifiers it gives a new declaration,
// and for the report each group's name and whether it is new.
type splitPlan struct {
	variable  *types.Var
	edits     []rename.Edit
	retargets map[rename.EditKey]int
	groups    []*group
	// insertions are the declarations the split adds, whose text is among its edits.
	insertions []rename.Insertion
	// residual says why the original keeps the name err, when it does.
	residual string
}

// splitter holds what the analysis of one package needs.
type splitter struct {
	fileSet      *token.FileSet
	packageTypes *types.Package
	info         *types.Info
	functions    *functionIndex
	occurrences  map[*types.Var][]*ast.Ident
	words        go_origin_names.Vocabulary
	// files are the package's editable files, for their comments and their text.
	files   []*ast.File
	sources map[string]string
}

// parameterHost is how a parameter or named result being split keeps its original variable: its function,
// and the name its rule gives it, or "" with the rule's category when it gives none.
type parameterHost struct {
	owner    ast.Node
	newName  string
	category string
	// result marks a named result, which every bare return reads.
	result bool
}

// split decides one variable. It returns a plan, or "" plans and a category saying why the variable is left.
// A plan with a single group is a plain rename of the original; the caller sends it to the rename engine. A
// parameter, written by its caller before the body runs, is split with parameter set.
func (analysis *splitter) split(variable *types.Var, parameter *parameterHost) (*splitPlan, string) {
	unit := analysis.functions.innermost(variable.Pos())
	if parameter != nil {
		unit = functionUnit{node: parameter.owner}
		switch owner := parameter.owner.(type) {
		case *ast.FuncDecl:
			unit.body = owner.Body
		case *ast.FuncLit:
			unit.body = owner.Body
		}
	}
	if unit.body == nil {
		return nil, "declared outside a function body"
	}
	occurrences := analysis.occurrences[variable]
	for _, occurrence := range occurrences {
		if parameter != nil && analysis.info.Defs[occurrence] == variable {
			continue
		}
		if analysis.functions.innermost(occurrence.Pos()).body != unit.body {
			return nil, "read or written inside a function literal"
		}
	}
	sites, communicationWrites, category := analysis.definitionSites(unit, variable)
	if category != "" {
		return nil, category
	}
	if parameter != nil {
		for _, occurrence := range occurrences {
			if analysis.info.Defs[occurrence] == variable {
				sites[occurrence] = definitionSite{identifier: occurrence, statement: parameter.owner, parameter: true}
			}
		}
	}
	webs, category := analysis.webs(unit, occurrences, sites, communicationWrites, parameter != nil && parameter.result)
	if category != "" {
		return nil, category
	}
	for _, each := range webs {
		analysis.nameWeb(each)
	}
	groups, category := analysis.group(unit, variable, webs, parameter)
	if category != "" {
		return nil, category
	}
	return analysis.plan(variable, groups), ""
}

// definitionSites finds every write of variable in unit, and the writes made by a select case's receive,
// which the graph places twice. It returns a category for a write it cannot place.
func (analysis *splitter) definitionSites(
	unit functionUnit,
	variable *types.Var,
) (map[*ast.Ident]definitionSite, map[*ast.Ident]bool, string) {
	sites := map[*ast.Ident]definitionSite{}
	communicationWrites := map[*ast.Ident]bool{}
	isVariable := func(expression ast.Expr) (*ast.Ident, bool) {
		identifier, isIdentifier := ast.Unparen(expression).(*ast.Ident)
		if !isIdentifier {
			return nil, false
		}
		return identifier, analysis.info.ObjectOf(identifier) == variable
	}
	category := ""
	ast.Inspect(unit.body, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.FuncLit:
			return false
		case *ast.AssignStmt:
			for index, left := range typed.Lhs {
				if identifier, written := isVariable(left); written {
					if typed.Tok != token.ASSIGN && typed.Tok != token.DEFINE {
						category = "written by an operator assignment"
					}
					sites[identifier] = definitionSite{identifier: identifier, statement: typed, index: index}
				}
			}
		case *ast.ValueSpec:
			for index, name := range typed.Names {
				if identifier, written := isVariable(name); written {
					sites[identifier] = definitionSite{identifier: identifier, statement: typed, index: index}
				}
			}
		case *ast.RangeStmt:
			for index, expression := range []ast.Expr{typed.Key, typed.Value} {
				if identifier, written := isVariable(expression); written {
					if typed.Tok != token.DEFINE {
						// The graph writes a range's key and value once, before the loop: right for a variable
						// the range declares, wrong for one it assigns, whose value from before the loop
						// survives an empty range.
						category = "assigned by a range clause"
					}
					sites[identifier] = definitionSite{identifier: identifier, statement: typed, index: index}
				}
			}
		case *ast.SelectStmt:
			for _, clause := range typed.Body.List {
				communication, isCommunication := clause.(*ast.CommClause)
				if !isCommunication {
					continue
				}
				if assignment, isAssignment := communication.Comm.(*ast.AssignStmt); isAssignment {
					for _, left := range assignment.Lhs {
						if identifier, written := isVariable(left); written {
							communicationWrites[identifier] = true
						}
					}
				}
			}
		}
		return true
	})
	return sites, communicationWrites, category
}

// event is one occurrence of the variable in a graph block, in evaluation order.
type event struct {
	identifier *ast.Ident
	// definition is the definition's number, -1 for a read, or bareReturn.
	definition int
}

// bareReturn marks a return without results, which reads every named result without naming it.
const bareReturn = -2

// webs runs reaching definitions over the unit's graph and joins every definition reaching one read.
//
// For a named result, a bare return reads it without naming it, so every write reaching one joins the
// result's own web, the one its function's entry begins.
func (analysis *splitter) webs(
	unit functionUnit,
	occurrences []*ast.Ident,
	sites map[*ast.Ident]definitionSite,
	communicationWrites map[*ast.Ident]bool,
	namedResult bool,
) ([]*web, string) {
	isOccurrence := map[*ast.Ident]bool{}
	for _, occurrence := range occurrences {
		isOccurrence[occurrence] = true
	}
	definitionNumber := map[*ast.Ident]int{}
	var definitions []definitionSite
	for _, occurrence := range occurrences {
		if site, isDefinition := sites[occurrence]; isDefinition {
			definitionNumber[occurrence] = len(definitions)
			definitions = append(definitions, site)
		}
	}
	graph := analysis.functions.graph(unit)
	blockEvents := make([][]event, len(graph.Blocks))
	seen := map[*ast.Ident]bool{}
	for identifier, number := range definitionNumber {
		if sites[identifier].parameter {
			// The caller writes a parameter before the first statement runs.
			blockEvents[0] = append(blockEvents[0], event{identifier: identifier, definition: number})
			seen[identifier] = true
		}
	}
	for _, block := range graph.Blocks {
		for _, node := range block.Nodes {
			// A select case's receive is listed twice: as a whole statement before the select, where only its
			// channel is evaluated, and as its bare left side in the case body, where it is written.
			_, isStatement := node.(*ast.AssignStmt)
			var reads []event
			var writes []event
			ast.Inspect(node, func(inner ast.Node) bool {
				if _, isLiteral := inner.(*ast.FuncLit); isLiteral {
					return false
				}
				identifier, isIdentifier := inner.(*ast.Ident)
				if !isIdentifier || !isOccurrence[identifier] {
					return true
				}
				seen[identifier] = true
				number, isDefinition := definitionNumber[identifier]
				switch {
				case !isDefinition:
					reads = append(reads, event{identifier: identifier, definition: -1})
				case isStatement && communicationWrites[identifier]:
				default:
					writes = append(writes, event{identifier: identifier, definition: number})
				}
				return true
			})
			if returned, isReturn := node.(*ast.ReturnStmt); isReturn && namedResult && len(returned.Results) == 0 {
				reads = append(reads, event{definition: bareReturn})
			}
			blockEvents[block.Index] = append(append(blockEvents[block.Index], reads...), writes...)
		}
	}
	for _, occurrence := range occurrences {
		if !seen[occurrence] {
			return nil, "an occurrence outside the control-flow graph"
		}
	}

	// Reaching definitions of the one variable: a block's last write is all that leaves it.
	predecessors := make([][]int, len(graph.Blocks))
	for _, block := range graph.Blocks {
		for _, successor := range block.Succs {
			predecessors[successor.Index] = append(predecessors[successor.Index], int(block.Index))
		}
	}
	allDefinitions := map[int]bool{}
	for number := range definitions {
		allDefinitions[number] = true
	}
	in := make([]map[int]bool, len(graph.Blocks))
	out := make([]map[int]bool, len(graph.Blocks))
	for index := range graph.Blocks {
		in[index], out[index] = map[int]bool{}, map[int]bool{}
	}
	lastWrite := func(index int) int {
		last := -1
		for _, each := range blockEvents[index] {
			if each.definition >= 0 {
				last = each.definition
			}
		}
		return last
	}
	for changed := true; changed; {
		changed = false
		for _, block := range graph.Blocks {
			index := int(block.Index)
			entering := map[int]bool{}
			for _, predecessor := range predecessors[index] {
				// Code no path reaches (after a return) reaches nothing either.
				if !graph.Blocks[predecessor].Live {
					continue
				}
				for number := range out[predecessor] {
					entering[number] = true
				}
			}
			leaving := entering
			if last := lastWrite(index); last >= 0 {
				leaving = map[int]bool{last: true}
			}
			if len(entering) != len(in[index]) || len(leaving) != len(out[index]) {
				in[index], out[index] = entering, leaving
				changed = true
			}
		}
	}

	parent := make([]int, len(definitions))
	for number := range parent {
		parent[number] = number
	}
	find := func(number int) int {
		root := number
		for parent[root] != root {
			parent[root] = parent[parent[root]]
			root = parent[root]
		}
		return root
	}
	entryDefinition := -1
	for identifier, number := range definitionNumber {
		if sites[identifier].parameter {
			entryDefinition = number
		}
	}
	usesOf := map[*ast.Ident][]int{}
	for _, block := range graph.Blocks {
		reaching := in[block.Index]
		if !block.Live {
			// A read in code no path reaches is joined to every write: an extra reach only merges webs.
			reaching = allDefinitions
		}
		for _, each := range blockEvents[block.Index] {
			if each.definition >= 0 {
				reaching = map[int]bool{each.definition: true}
				continue
			}
			if each.definition == bareReturn {
				for number := range reaching {
					parent[find(number)] = find(entryDefinition)
				}
				continue
			}
			if len(reaching) == 0 {
				return nil, "a read no write reaches"
			}
			var numbers []int
			for number := range reaching {
				numbers = append(numbers, number)
			}
			sort.Ints(numbers)
			for _, number := range numbers[1:] {
				parent[find(number)] = find(numbers[0])
			}
			usesOf[each.identifier] = numbers
		}
	}

	byRoot := map[int]*web{}
	var webs []*web
	for number, site := range definitions {
		root := find(number)
		if byRoot[root] == nil {
			byRoot[root] = &web{}
			webs = append(webs, byRoot[root])
		}
		byRoot[root].definitions = append(byRoot[root].definitions, site)
	}
	for _, occurrence := range occurrences {
		if numbers, isUse := usesOf[occurrence]; isUse {
			byRoot[find(numbers[0])].uses = append(byRoot[find(numbers[0])].uses, occurrence)
		}
	}
	sort.SliceStable(webs, func(left, right int) bool { return webs[left].first() < webs[right].first() })
	return webs, ""
}

// writtenValue is the expression a definition writes into the variable, or nil for a declaration without
// a value.
func writtenValue(site definitionSite) ast.Expr {
	var values []ast.Expr
	var count int
	switch statement := site.statement.(type) {
	case *ast.AssignStmt:
		values, count = statement.Rhs, len(statement.Lhs)
	case *ast.ValueSpec:
		values, count = statement.Values, len(statement.Names)
	default:
		return nil
	}
	switch {
	case len(values) == 0:
		return nil
	case len(values) == count:
		return values[site.index]
	}
	return values[0]
}

// nameWeb names a web from the calls that write it: the one name they agree on, or a category.
// Mutates: each naming a web is filling in its name, or its category and the calls behind it
func (analysis *splitter) nameWeb(each *web) {
	names := map[string]bool{}
	var calls []string
	each.neutral = true
	for _, definition := range each.definitions {
		if definition.parameter {
			continue
		}
		value := writtenValue(definition)
		if _, isDeclaration := definition.statement.(*ast.ValueSpec); isDeclaration && value == nil {
			continue
		}
		if identifier, isIdentifier := ast.Unparen(
			value,
		).(*ast.Ident); isIdentifier && analysis.info.Uses[identifier] == types.Universe.Lookup("nil") {
			continue
		}
		each.neutral = false
		if _, isRange := definition.statement.(*ast.RangeStmt); isRange {
			each.category = "defined by a range clause"
			return
		}
		call, isCall := ast.Unparen(value).(*ast.CallExpr)
		if received := receivedErrorName(value); !isCall && received != "" {
			names[received] = true
			calls = append(calls, "received from a channel")
			continue
		}
		if !isCall {
			each.category = "a write is not a call"
			return
		}
		newName, category, callName := go_origin_names.CallErrorName(analysis.info, call, analysis.words)
		if newName == "" {
			each.category = category
			return
		}
		names[newName] = true
		calls = append(calls, callName)
	}
	if len(names) > 1 {
		each.category = "written by calls that name it differently"
		each.calls = calls
		return
	}
	for name := range names {
		each.newName = name
	}
}

// declaringScope is the scope a short variable declaration at statement would declare in.
func (analysis *splitter) declaringScope(statement ast.Node) *types.Scope {
	return analysis.packageTypes.Scope().Innermost(statement.Pos())
}

// canDeclare reports whether site's statement can declare a new variable for its identifier: a short
// declaration already, or a plain assignment whose every other target is a name the statement's own scope
// declares (so `:=` reuses it) or blank. A for statement's post statement cannot declare.
func (analysis *splitter) canDeclare(site definitionSite, forPosts map[ast.Node]bool) bool {
	assignment, isAssignment := site.statement.(*ast.AssignStmt)
	if !isAssignment || forPosts[assignment] {
		return false
	}
	if assignment.Tok == token.DEFINE {
		return true
	}
	scope := analysis.declaringScope(assignment)
	for index, left := range assignment.Lhs {
		if index == site.index {
			continue
		}
		identifier, isIdentifier := left.(*ast.Ident)
		if !isIdentifier {
			return false
		}
		if identifier.Name == "_" {
			continue
		}
		object := analysis.info.Uses[identifier]
		if object == nil || object.Parent() != scope {
			return false
		}
	}
	return true
}

// covers reports whether a new variable visible in scope from start, declared at anchor (an identifier, or
// nil for an inserted declaration), is visible at every occurrence of a web, and whether every short declaration among
// the web's definitions is in that same scope, where it reuses the variable instead of declaring another.
func (analysis *splitter) covers(start token.Pos, anchor *ast.Ident, scope *types.Scope, each *web) bool {
	inside := func(position token.Pos) bool {
		return scope.Pos() <= position && position < scope.End()
	}
	for _, definition := range each.definitions {
		if definition.identifier == anchor {
			continue
		}
		if definition.identifier.Pos() < start || !inside(definition.identifier.Pos()) {
			return false
		}
		if assignment, isAssignment := definition.statement.(*ast.AssignStmt); isAssignment &&
			assignment.Tok == token.DEFINE &&
			analysis.declaringScope(assignment) != scope {
			return false
		}
	}
	for _, use := range each.uses {
		if use.Pos() < start || !inside(use.Pos()) {
			return false
		}
	}
	return true
}

// group places the webs into variables. The web holding the declaration keeps the original variable, and a
// web written only by the declaration's zero value and nil (`var err error`, then writes) leaves the
// original free for the first web whose claim lets every web be placed. Every other web joins a variable of its own
// name when that variable is visible at all of it, or else becomes a new variable declared at its first write.
// A web written by nothing but zero values and nil joins the original. A web whose writes are never read
// cannot be a new variable, which Go would refuse as unused, so it must join one of its own name.
//
// A parameter's web keeps the original under the name the parameter rule gives it. A parameter needs no read;
// Go lets one go unread.
func (analysis *splitter) group(
	unit functionUnit,
	variable *types.Var,
	webs []*web,
	parameter *parameterHost,
) ([]*group, string) {
	forPosts := map[ast.Node]bool{}
	ast.Inspect(unit.body, func(node ast.Node) bool {
		if loop, isLoop := node.(*ast.ForStmt); isLoop && loop.Post != nil {
			forPosts[loop.Post] = true
		}
		return true
	})
	var hostWeb *web
	for _, each := range webs {
		for _, definition := range each.definitions {
			if analysis.info.Defs[definition.identifier] == variable {
				hostWeb = each
			}
		}
	}
	if hostWeb == nil {
		return nil, "no write declares it"
	}
	floating := hostWeb.neutral && parameter == nil
	if parameter != nil {
		// A body write left in the parameter's web keeps reassigning it, and its reassignment finding is
		// re-worded under the new name, which the ruling accepts.
		hostWeb.neutral, hostWeb.newName, hostWeb.category = false, parameter.newName, parameter.category
	}

	// attempt places every web with the original claimed by claimer (nil: by the declaration's own web).
	attempt := func(claimer *web) ([]*group, string) {
		host := &group{host: true, webs: []*web{hostWeb}}
		switch {
		case claimer != nil:
			host.newName, host.webs = claimer.newName, []*web{hostWeb, claimer}
			if claimer.newName == "" {
				host.newName = "err"
			}
		case hostWeb.newName != "":
			host.newName = hostWeb.newName
		case !hostWeb.neutral:
			host.newName = "err"
		}
		groups := []*group{host}
		for _, each := range webs {
			if each == hostWeb || each == claimer {
				continue
			}
			name := each.newName
			if name == "" && !each.neutral {
				name = "err"
			}
			if each.neutral {
				host.webs = append(host.webs, each)
				continue
			}
			joined := false
			for _, existing := range groups {
				if existing.newName != name {
					continue
				}
				if existing.host ||
					analysis.covers(existing.visibleFrom, existing.anchor.identifier, existing.scope, each) {
					existing.webs = append(existing.webs, each)
					joined = true
					break
				}
			}
			if joined {
				continue
			}
			if len(each.uses) == 0 {
				return nil, fmt.Sprintf(
					"a write at line %d is never read",
					analysis.fileSet.Position(each.definitions[0].identifier.Pos()).Line,
				)
			}
			anchor := each.definitions[0]
			for _, definition := range each.definitions[1:] {
				if definition.identifier.Pos() < anchor.identifier.Pos() {
					anchor = definition
				}
			}
			scope := analysis.declaringScope(anchor.statement)
			if analysis.canDeclare(anchor, forPosts) &&
				analysis.covers(anchor.statement.End(), anchor.identifier, scope, each) &&
				scope.Lookup(name) == nil {
				groups = append(groups, &group{
					newName:     name,
					webs:        []*web{each},
					anchor:      anchor,
					scope:       scope,
					visibleFrom: anchor.statement.End(),
				})
				continue
			}
			// The write cannot declare the variable itself, or its declaration would not reach every read: declare
			// it with an inserted `var` instead.
			declared, category := analysis.insertedDeclaration(unit, anchor, each, name)
			if category != "" {
				return nil, category
			}
			groups = append(groups, declared)
		}
		if host.newName == "" {
			host.newName = "err"
		}
		// Go refuses a variable nothing reads, so the original must keep a read once others leave it.
		hostReads := 0
		for _, member := range host.webs {
			hostReads += len(member.uses)
		}
		if hostReads == 0 && len(groups) > 1 && parameter == nil {
			return nil, fmt.Sprintf(
				"a write at line %d is never read",
				analysis.fileSet.Position(hostWeb.definitions[0].identifier.Pos()).Line,
			)
		}
		return groups, ""
	}
	if !floating {
		return attempt(nil)
	}
	firstCategory := ""
	for _, claimer := range webs {
		if claimer == hostWeb || claimer.neutral {
			continue
		}
		groups, category := attempt(claimer)
		if category == "" {
			return groups, ""
		}
		if firstCategory == "" {
			firstCategory = category
		}
	}
	if firstCategory == "" {
		return attempt(nil)
	}
	return nil, firstCategory
}

// plan turns the groups into edits: every occurrence takes its group's name, a new variable's anchor becomes
// a short declaration, and every occurrence of a new variable is retargeted to its anchor.
func (analysis *splitter) plan(variable *types.Var, groups []*group) *splitPlan {
	result := &splitPlan{variable: variable, retargets: map[rename.EditKey]int{}, groups: groups}
	offset := func(position token.Pos) token.Position { return analysis.fileSet.PositionFor(position, false) }
	for _, each := range groups {
		var identifiers []*ast.Ident
		for _, member := range each.webs {
			for _, definition := range member.definitions {
				identifiers = append(identifiers, definition.identifier)
			}
			identifiers = append(identifiers, member.uses...)
		}
		for _, identifier := range identifiers {
			position := offset(identifier.Pos())
			if each.newName != identifier.Name {
				result.edits = append(result.edits, rename.Edit{
					Filename: position.Filename,
					Offset:   position.Offset,
					OldText:  identifier.Name,
					NewText:  each.newName,
				})
			}
			switch {
			case each.insertion != nil:
				result.retargets[rename.EditKey{
					Filename: position.Filename,
					Offset:   position.Offset,
				}] = rename.InsertedDeclaration(each.insertion.Offset)
			case !each.host:
				result.retargets[rename.EditKey{
					Filename: position.Filename,
					Offset:   position.Offset,
				}] = offset(each.anchor.identifier.Pos()).Offset
			}
		}
		if !each.host {
			each.anchorOffset = offset(each.anchor.identifier.Pos()).Offset
		}
		if each.insertion != nil {
			result.insertions = append(result.insertions, *each.insertion)
			result.edits = append(
				result.edits,
				rename.Edit{Filename: each.insertion.Filename, Offset: each.insertion.Offset,
					NewText: each.indent + "var " + each.newName + " " + each.insertion.TypeName + "\n"},
			)
		}
		if assignment, isAssignment := each.anchor.statement.(*ast.AssignStmt); !each.host &&
			each.insertion == nil &&
			isAssignment &&
			assignment.Tok == token.ASSIGN {
			position := offset(assignment.TokPos)
			result.edits = append(
				result.edits,
				rename.Edit{Filename: position.Filename, Offset: position.Offset, OldText: "=", NewText: ":="},
			)
			// A blank on the left of a short declaration declares a blank variable of its own.
			for _, left := range assignment.Lhs {
				if identifier, isIdentifier := left.(*ast.Ident); isIdentifier && identifier.Name == "_" {
					blank := offset(identifier.Pos())
					result.retargets[rename.EditKey{Filename: blank.Filename, Offset: blank.Offset}] = blank.Offset
				}
			}
		}
		if each.newName == "err" && each.host {
			result.residual = "the original keeps err: " + residualReason(each)
		} else if each.newName == "err" {
			result.residual = "a new variable keeps err: " + residualReason(each)
		}
	}
	return result
}

// insertedDeclaration declares a web's new variable with `var name error` inserted on a line of its own right
// before the statement holding its first write, in the narrowest block that holds every occurrence: a write
// into a field or an outer variable beside the error cannot declare it, and a write in a branch read after the
// branch would declare it too narrowly. The statement must open its line; a comment directly above it stays
// with it, so the declaration goes above the comment.
func (analysis *splitter) insertedDeclaration(
	unit functionUnit,
	anchor definitionSite,
	each *web,
	name string,
) (*group, string) {
	line := analysis.fileSet.Position(anchor.identifier.Pos()).Line
	positions := []token.Pos{}
	for _, definition := range each.definitions {
		positions = append(positions, definition.identifier.Pos())
	}
	for _, use := range each.uses {
		positions = append(positions, use.Pos())
	}
	var block ast.Node
	var statements []ast.Stmt
	ast.Inspect(unit.body, func(node ast.Node) bool {
		if _, isLiteral := node.(*ast.FuncLit); isLiteral {
			return false
		}
		var list []ast.Stmt
		switch typed := node.(type) {
		case *ast.BlockStmt:
			list = typed.List
		case *ast.CaseClause:
			list = typed.Body
		case *ast.CommClause:
			list = typed.Body
		default:
			return true
		}
		for _, position := range positions {
			if position < node.Pos() || position >= node.End() {
				return true
			}
		}
		block, statements = node, list
		return true
	})
	var before ast.Stmt
	for _, statement := range statements {
		if statement.Pos() <= anchor.identifier.Pos() && anchor.identifier.Pos() < statement.End() {
			before = statement
		}
	}
	if before == nil {
		return nil, fmt.Sprintf("no block statement holds the write at line %d and every read", line)
	}
	for _, position := range positions {
		if position < before.Pos() {
			return nil, fmt.Sprintf("a read comes before the write at line %d", line)
		}
	}
	scope := analysis.info.Scopes[block]
	if block == unit.body {
		switch owner := unit.node.(type) {
		case *ast.FuncDecl:
			scope = analysis.info.Scopes[owner.Type]
		case *ast.FuncLit:
			scope = analysis.info.Scopes[owner.Type]
		}
	}
	if scope == nil {
		return nil, fmt.Sprintf("the block holding line %d has no scope", line)
	}
	if scope.Lookup(name) != nil {
		return nil, fmt.Sprintf("%s is already declared in the block holding line %d", name, line)
	}
	if !analysis.covers(before.Pos(), nil, scope, each) {
		return nil, fmt.Sprintf("a variable declared before line %d would not reach all its reads", line)
	}
	tokenFile := analysis.fileSet.File(before.Pos())
	source := analysis.source(tokenFile.Name())
	statementLine := tokenFile.Line(before.Pos())
	insertLine := statementLine
	for _, file := range analysis.files {
		if analysis.fileSet.File(file.Pos()) != tokenFile {
			continue
		}
		for moved := true; moved; {
			moved = false
			for _, comments := range file.Comments {
				if tokenFile.Line(comments.End()) == insertLine-1 && tokenFile.Line(comments.Pos()) < insertLine {
					insertLine, moved = tokenFile.Line(comments.Pos()), true
				}
			}
		}
	}
	lineStart := tokenFile.Offset(tokenFile.LineStart(insertLine))
	statementStart := tokenFile.Offset(tokenFile.LineStart(statementLine))
	indent := source[statementStart:tokenFile.Offset(before.Pos())]
	if strings.TrimLeft(indent, " \t") != "" {
		return nil, fmt.Sprintf("the statement at line %d does not open its line", statementLine)
	}
	return &group{
		newName:     name,
		webs:        []*web{each},
		anchor:      anchor,
		scope:       scope,
		visibleFrom: before.Pos(),
		insertion:   &rename.Insertion{Filename: tokenFile.Name(), Offset: lineStart, Name: name, TypeName: "error"},
		indent:      indent,
	}, ""
}

// source is a file's text, read once.
func (analysis *splitter) source(filename string) string {
	if analysis.sources == nil {
		analysis.sources = map[string]string{}
	}
	if text, read := analysis.sources[filename]; read {
		return text
	}
	contents, readError := os.ReadFile(filename)
	if readError != nil {
		// Ignored: an unreadable file reads as empty, so no statement in it opens its line and nothing is inserted.
		contents = nil
	}
	analysis.sources[filename] = string(contents)
	return analysis.sources[filename]
}

// residualReason says why a group named err could not be named.
func residualReason(each *group) string {
	for _, member := range each.webs {
		if member.category != "" {
			return member.category
		}
	}
	return "written only by zero values and nil"
}
