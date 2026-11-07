package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/token"
	"sort"
	"strings"
)

// lineLimit is the most lines a file may hold. Methods join their type's declaration file only while
// it stays at or under it, and nothing joins an existing file past it.
const lineLimit = 2000

// partsOf lists, per file, the parts it holds in source order of their first appearance.
func partsOf(group *packageGroup) map[*sourceFile][]*part {
	records := typesOf(group)
	withMethods := map[string]bool{}
	ambiguous := map[string]bool{}
	recordOf := map[*ast.TypeSpec]*typeRecord{}
	anyRecord := map[string]*typeRecord{}
	// methodOf is the counted type each method the build compiles belongs to, its receiver read
	// through local aliases by typesOf.
	methodOf := map[*ast.FuncDecl]string{}
	for _, record := range records {
		recordOf[record.spec] = record
		if !record.counted() {
			continue
		}
		for _, method := range record.methods {
			methodOf[method.declaration] = record.name
		}
		withMethods[record.name] = true
		ambiguous[record.name] = ambiguous[record.name] || record.ambiguous
		if anyRecord[record.name] == nil {
			anyRecord[record.name] = record
		}
	}
	result := map[*sourceFile][]*part{}
	for _, file := range group.files {
		byName := map[string]*part{}
		partFor := func(name string) *part {
			piece := byName[name]
			if piece == nil {
				piece = &part{name: name, file: file, record: anyRecord[name], ambiguous: ambiguous[name]}
				byName[name] = piece
				result[file] = append(result[file], piece)
			}
			return piece
		}
		for _, declaration := range file.syntax.Decls {
			switch node := declaration.(type) {
			case *ast.GenDecl:
				if node.Tok != token.TYPE {
					continue
				}
				for _, spec := range node.Specs {
					typeSpec, isTypeSpec := spec.(*ast.TypeSpec)
					if !isTypeSpec {
						continue
					}
					record := recordOf[typeSpec]
					if record == nil || !withMethods[record.name] {
						continue
					}
					piece := partFor(record.name)
					piece.declares = true
					piece.record = record
				}
			case *ast.FuncDecl:
				if name := methodOf[node]; withMethods[name] {
					piece := partFor(name)
					piece.methods = append(piece.methods, node)
				}
			}
		}
	}
	return result
}

// chooseKeeper picks the part that stays in its file: a type that cannot move, else the type the file
// is named for when the file declares it, else the part with the most lines, the one named for the
// file on a tie. A file named for a type whose declaration lives elsewhere keeps that type's methods
// only while they are its largest part; otherwise they join their declaration, and the file goes to
// the type it does declare, which costs less moving and leaves each type beside its declaration.
func chooseKeeper(file *sourceFile, parts []*part) *part {
	stem := fileStem(file.name)
	namedForFile := func(piece *part) bool {
		return namesType(strings.TrimSuffix(stem, "_test"), piece.name, isToolchainSuffix) ||
			namesType(strings.TrimSuffix(stem, "_test"), piece.name, isVariantSuffix)
	}
	for _, piece := range parts {
		if piece.ambiguous || piece.readsRuleProgram() {
			return piece
		}
	}
	for _, piece := range parts {
		if piece.declares && namedForFile(piece) {
			return piece
		}
	}
	keeper := parts[0]
	largest := -1
	for _, piece := range parts {
		size := piece.lineCount()
		if size > largest || (size == largest && namedForFile(piece)) {
			keeper, largest = piece, size
		}
	}
	return keeper
}

// programReadReason is why a part that readsRuleProgram stays.
const programReadReason = "it reads the program through a rule.Context, which internal/guard's " +
	"TestRulesDeclareExactlyWhatTheyReadOfTheProgram keeps in the file that defines the rule"

// planGroup makes every file of a group serve at most one type. In a file serving several, one part
// stays and each other part leaves. A part holding the type's declaration goes to snake_case(Type).go,
// or joins an existing file that serves only that type. A part of methods alone joins the type's
// declaration file while that stays within lineLimit, else a themed file snake_case(Type)_<base>.go.
// Mutates: taken each file name the plan takes joins the directory's names, so no two plans take one
// Mutates: result the directory's counts and conflicts gather every group's plan
func planGroup(group *packageGroup, taken map[string]bool, result *directoryResult) *groupPlan {
	plan := &groupPlan{
		removals: map[*sourceFile][]byteRange{},
		appends:  map[string][]chunk{},
		created:  map[string]*sourceFile{},
	}
	parts := partsOf(group)
	filesByName := map[string]*sourceFile{}
	// served, lines and keys track every file of the group, and every file the plan makes, as the plan
	// will leave them.
	served := map[string]map[string]bool{}
	lines := map[string]int{}
	keys := map[string]string{}
	declarationFile := map[string]string{}
	var leaving []*part
	conflict := func(piece *part, reason string) {
		result.conflicts = append(
			result.conflicts,
			fmt.Sprintf("%s/%s: %s stays, %s", group.directory, piece.file.name, piece.name, reason),
		)
		served[piece.file.name][piece.name] = true
		if piece.declares {
			declarationFile[piece.name] = piece.file.name
		}
	}
	for _, file := range group.files {
		filesByName[file.name] = file
		served[file.name] = map[string]bool{}
		lines[file.name] = bytes.Count(file.source, []byte("\n"))
		keys[file.name] = file.key
	}
	// A file the file name rule flags whose one type is a minority of it keeps its name, and the type
	// leaves for a file of its own, as a second type would: arrow_body_style.go stays the rule's file and
	// its options type goes to arrow_body_style_options.go, where a reader looks for it.
	extracting := map[*sourceFile]bool{}
	for _, misnamed := range misnamedFiles(group) {
		if misnamed.extractable() {
			extracting[misnamed.file] = true
			result.extractionsTried[misnamed.file.name] = true
		}
	}
	for _, file := range group.files {
		fileParts := parts[file]
		if extracting[file] {
			if piece := fileParts[0]; piece.readsRuleProgram() {
				conflict(piece, programReadReason)
				continue
			}
			leaving = append(leaving, fileParts...)
			result.extractions++
			continue
		}
		if file.pinned != "" || len(fileParts) < 2 {
			for _, piece := range fileParts {
				served[file.name][piece.name] = true
				if piece.declares {
					declarationFile[piece.name] = file.name
				}
			}
			continue
		}
		keeper := chooseKeeper(file, fileParts)
		served[file.name][keeper.name] = true
		if keeper.declares {
			declarationFile[keeper.name] = file.name
		}
		for _, piece := range fileParts {
			if piece == keeper {
				continue
			}
			if piece.ambiguous {
				conflict(piece, "it is declared once per build")
				continue
			}
			if piece.readsRuleProgram() {
				conflict(piece, programReadReason)
				continue
			}
			leaving = append(leaving, piece)
		}
	}

	// accepts says whether a file of the group, or one the plan makes, can take a part: built under the
	// same constraints, not pinned, serving no other type, and within lineLimit with it.
	accepts := func(name string, piece *part) bool {
		if name == piece.file.name || served[name] == nil || keys[name] != piece.file.key {
			return false
		}
		if target := filesByName[name]; target != nil && target.pinned != "" {
			return false
		}
		for other := range served[name] {
			if other != piece.name {
				return false
			}
		}
		return lines[name]+piece.lineCount()+len(piece.methods)+2 <= lineLimit
	}
	nameProblem := func(name string, suffix string) string {
		_, checkSuffix := fileNameParts(name)
		switch {
		case checkSuffix != suffix:
			return name + " would carry other build constraints"
		case strings.HasPrefix(name, "_") || strings.HasPrefix(name, "."):
			return name + " would be ignored by the go command"
		case taken[strings.ToLower(name)] && served[name] == nil:
			return name + " exists outside this package, or differs only in case"
		}
		return ""
	}
	destination := map[*part]string{}
	send := func(piece *part, name string) {
		if served[name] == nil {
			taken[strings.ToLower(name)] = true
			plan.created[name] = piece.file
			served[name] = map[string]bool{}
			keys[name] = piece.file.key
		}
		served[name][piece.name] = true
		lines[name] += piece.lineCount() + len(piece.methods) + 2
		destination[piece] = name
	}

	// Declarations first, so methods that follow a declaration know where it ends up.
	for _, piece := range leaving {
		if !piece.declares {
			continue
		}
		name := typeFileName(piece.name, piece.file)
		_, suffix := fileNameParts(piece.file.name)
		if problem := nameProblem(name, suffix); problem != "" {
			conflict(piece, problem)
			continue
		}
		// The file named for the type, existing or new, comes before a themed file of its methods, which the file
		// name rule accepts only while it holds no declaration.
		if accepts(name, piece) || (served[name] == nil && !taken[strings.ToLower(name)]) {
			send(piece, name)
			declarationFile[piece.name] = name
			result.typeMoves++
			continue
		}
		// An existing file that already serves only this type, such as a themed file of its methods.
		themed := ""
		for _, file := range group.files {
			if served[file.name][piece.name] && accepts(file.name, piece) {
				themed = file.name
				break
			}
		}
		switch {
		case themed != "":
			send(piece, themed)
			declarationFile[piece.name] = themed
			result.typeMoves++
		default:
			conflict(piece, name+
				" serves another type, is built under other constraints, is pinned, or would pass "+
				fmt.Sprint(lineLimit)+
				" lines")
		}
	}
	for _, piece := range leaving {
		if piece.declares {
			continue
		}
		if home, ok := declarationFile[piece.name]; ok && accepts(home, piece) {
			send(piece, home)
			result.methodMovesToDeclaration++
			continue
		}
		name := themedName(piece.name, piece.file.name)
		if name == "" {
			conflict(piece, "its declaration's file cannot take it and "+piece.file.name+" names no theme apart from "+
				piece.name)
			continue
		}
		_, suffix := fileNameParts(piece.file.name)
		if problem := nameProblem(name, suffix); problem != "" {
			conflict(piece, problem)
			continue
		}
		switch {
		case accepts(name, piece):
			send(piece, name)
			result.methodMovesToThemed++
		case served[name] == nil && !taken[strings.ToLower(name)]:
			send(piece, name)
			result.methodMovesToThemed++
		default:
			conflict(piece, name+
				" serves another type, is built under other constraints, is pinned, or would pass "+
				fmt.Sprint(lineLimit)+
				" lines")
		}
	}

	// A grouped type declaration that loses every spec is removed whole, and its doc travels with the
	// first spec to leave.
	specsLeaving := map[*ast.GenDecl]int{}
	for piece := range destination {
		if piece.declares {
			specsLeaving[piece.record.declaration]++
		}
	}
	groupDocCarried := map[*ast.GenDecl]bool{}
	ordered := make([]*part, 0, len(destination))
	for _, piece := range leaving {
		if _, ok := destination[piece]; ok {
			ordered = append(ordered, piece)
		}
	}
	sort.SliceStable(ordered, func(left, right int) bool {
		return ordered[left].declares && !ordered[right].declares
	})
	for _, piece := range ordered {
		file := piece.file
		target := destination[piece]
		var moved []byteRange
		if piece.declares {
			declaration := piece.record.declaration
			var text []byte
			if declaration.Lparen == token.NoPos {
				span := file.lineRange(docStart(declaration.Doc, declaration.Pos()), declaration.End())
				text = file.source[span.start:span.end]
				plan.removals[file] = append(plan.removals[file], span)
				moved = append(moved, span)
			} else {
				var builder bytes.Buffer
				whole := specsLeaving[declaration] == len(declaration.Specs)
				if whole && !groupDocCarried[declaration] {
					groupDocCarried[declaration] = true
					if declaration.Doc != nil {
						builder.Write(file.text(declaration.Doc.Pos(), declaration.Doc.End()))
						builder.WriteString("\n")
					}
					span := file.lineRange(docStart(declaration.Doc, declaration.Pos()), declaration.End())
					plan.removals[file] = append(plan.removals[file], span)
				}
				spec := piece.record.spec
				if spec.Doc != nil {
					builder.Write(file.text(spec.Doc.Pos(), spec.Doc.End()))
					builder.WriteString("\n")
				}
				builder.WriteString("type ")
				builder.Write(file.source[file.offset(spec.Pos()):file.trailingEnd(file.offset(spec.End()))])
				builder.WriteString("\n")
				text = builder.Bytes()
				span := file.lineRange(docStart(spec.Doc, spec.Pos()), spec.End())
				if !whole {
					plan.removals[file] = append(plan.removals[file], span)
				}
				moved = append(moved, span)
			}
			plan.appends[target] = append(plan.appends[target], chunk{text: text, source: file})
		}
		for _, method := range piece.methods {
			span := file.lineRange(docStart(method.Doc, method.Pos()), method.End())
			plan.removals[file] = append(plan.removals[file], span)
			plan.appends[target] = append(
				plan.appends[target],
				chunk{text: file.source[span.start:span.end], source: file},
			)
			moved = append(moved, span)
		}
		result.families = append(result.families, familyCandidates(group, file, piece.name, target, moved)...)
	}
	return plan
}

// typeFileName is the name of the file a type moves to out of origin: the type's name in snake_case with the
// suffix the file name rule keeps, origin's toolchain suffix, or in a file with a //go:build line its last word,
// which names the variant, and _test for a test file.
func typeFileName(typeName string, origin *sourceFile) string {
	stem := fileStem(origin.name)
	testStem := strings.TrimSuffix(stem, "_test")
	name := snakeCase(typeName) + variantSuffix(testStem, hasBuildConstraint(origin.syntax))
	if testStem != stem {
		name += "_test"
	}
	return name + ".go"
}

// isRuleSelector is whether expression is `rule.<name>`.
func isRuleSelector(expression ast.Expr, name string) bool {
	selector, isSelector := expression.(*ast.SelectorExpr)
	if !isSelector {
		return false
	}
	packageName, isIdentifier := selector.X.(*ast.Ident)
	return isIdentifier && packageName.Name == "rule" && selector.Sel.Name == name
}
