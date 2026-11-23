// This file is the spike's variability (#yraxcp9): one check of a package for every build at once. It is cohere's,
// not upstream's, and every other file of the fork reaches into it at a few marked sites.
//
// The model. A build set is a Mask, one bit per build. A package-level object keeps one identity across every build
// that declares it, as Go's own rule for a declared type has it (one declaration, one type), and only its properties
// may differ: a constant's value, a variable's or function's type, a named type's underlying type, and in which builds
// it exists at all. The checker runs each unit of work (a package-level declaration, a delayed action such as a
// function body) under the builds it holds in. When a read inside a unit would answer differently across those builds,
// the read panics with a split, and the unit starts again once for each group of builds that answer alike. A unit
// that never reads a differing property runs once for all of its builds, which is the shared case and nearly every
// case.
//
// The spike checks one package at a time, on one goroutine, so the build set a unit runs under is held here, in
// package state, and the reads that never see a Checker (Named.Underlying above all) can reach it. Threading it
// through the checker instead is the full design's work, sized in the spike's report.

package types

import (
	"go/ast"
	"go/constant"
	"math/bits"
)

// Mask is a set of builds, bit b for build b. The spike holds at most eight.
type Mask uint8

// Variation is what a variational check is given and what it answers. Builds is every build the check covers, and
// Files says which of them compile each file, a file it doesn't name compiling in every one. Types is the answer:
// every expression's type and value, with the builds each holds in.
type Variation struct {
	Builds Mask
	Files  map[*ast.File]Mask
	Types  map[ast.Expr][]BuildTypeAndValue

	// Counts of the check's work, for the spike's report.
	Units    int // units run to their end
	Restarts int // unit runs abandoned for a split
}

// BuildTypeAndValue is an expression's answer in Builds.
type BuildTypeAndValue struct {
	Builds Mask
	TypeAndValue
}

// variational is the state of the variational check in progress, if any.
var variational struct {
	active    bool
	all       Mask
	mask      Mask // the builds the current unit runs under
	depth     int  // how many units are running; a read outside every unit never splits
	narrowed  uint64
	variation *Variation
}

// splitRequest is the panic a read raises when its answer differs across the current unit's builds: parts are the
// groups of builds that answer alike, and together they are the unit's builds.
type splitRequest struct{ parts []Mask }

// objectVariation is what a package-level object holds when it doesn't hold the same in every build.
type objectVariation struct {
	presence Mask            // the builds it exists in; zero for every build
	variants []objectVariant // its answers by build once declared; nil while one answer holds everywhere
	decls    []declVariant   // a source object declared in more than one file: each declaration and its builds
	named    *Named          // a type name's one named type, kept across its declarations
}

type objectVariant struct {
	mask Mask
	typ  Type
	val  constant.Value
}

type declVariant struct {
	mask Mask
	decl *declInfo
	iota constant.Value // a constant's iota in this declaration
}

// namedVariation is a named type's underlying type by build, when it differs.
type namedVariation struct {
	variants []namedVariant
}

type namedVariant struct {
	mask       Mask
	rhs        Type
	underlying Type
}

// variationOf is obj's variation, or nil.
func variationOf(obj Object) *objectVariation {
	if base, isBase := obj.(interface{ base() *object }); isBase {
		return base.base().variation
	}
	return nil
}

func (obj *object) base() *object { return obj }

// ensureVariation is obj's variation, made when it has none.
func (obj *object) ensureVariation() *objectVariation {
	if obj.variation == nil {
		obj.variation = &objectVariation{}
	}
	return obj.variation
}

// Variant is one answer of an imported object, for the importer that merges each build's export data.
type Variant struct {
	Builds Mask
	Type   Type
	Value  constant.Value
}

// SetVariants gives obj its answers by build, which the merging importer read from each build's export data. Answers
// alike are kept as one. obj's own type and value are set to the first answer's.
func SetVariants(obj Object, variants []Variant) {
	base := obj.(interface{ base() *object }).base()
	collapsed := make([]objectVariant, 0, len(variants))
	for _, variant := range variants {
		collapsed = appendVariant(collapsed, objectVariant{variant.Builds, variant.Type, variant.Value})
	}
	if len(collapsed) == 0 {
		return
	}
	base.typ = collapsed[0].typ
	if constantObject, isConstant := obj.(*Const); isConstant {
		constantObject.val = collapsed[0].val
	}
	if len(collapsed) > 1 {
		base.ensureVariation().variants = collapsed
	}
}

// SetPresence says obj exists only in builds.
func SetPresence(obj Object, builds Mask) {
	base := obj.(interface{ base() *object }).base()
	base.ensureVariation().presence = builds
}

// UnderlyingVariant is one of a named type's underlying types, for the merging importer.
type UnderlyingVariant struct {
	Builds     Mask
	Underlying Type
}

// SetUnderlyingVariants gives n its underlying types by build. Alike ones are kept as one, and when one is left n is
// left as it is.
func SetUnderlyingVariants(n *Named, variants []UnderlyingVariant) {
	var kept []namedVariant
	for _, variant := range variants {
		merged := false
		for index := range kept {
			if Identical(kept[index].rhs, variant.Underlying) {
				kept[index].mask |= variant.Builds
				merged = true
				break
			}
		}
		if !merged {
			kept = append(kept, namedVariant{mask: variant.Builds, rhs: variant.Underlying, underlying: variant.Underlying})
		}
	}
	if len(kept) > 1 {
		n.variation = &namedVariation{variants: kept}
	}
}

// VariantsOf is obj's answers by build, or nil when one answer holds in every build.
func VariantsOf(obj Object) []Variant {
	variation := variationOf(obj)
	if variation == nil || len(variation.variants) == 0 {
		return nil
	}
	variants := make([]Variant, len(variation.variants))
	for index, variant := range variation.variants {
		variants[index] = Variant{Builds: variant.mask, Type: variant.typ, Value: variant.val}
	}
	return variants
}

// PresenceOf is the builds obj exists in, or zero when it exists in every build.
func PresenceOf(obj Object) Mask {
	variation := variationOf(obj)
	if variation == nil {
		return 0
	}
	return variation.presence
}

// UnderlyingVariantsOf is n's underlying types by build, or nil when one holds in every build.
func UnderlyingVariantsOf(n *Named) []UnderlyingVariant {
	if n.variation == nil {
		return nil
	}
	variants := make([]UnderlyingVariant, len(n.variation.variants))
	for index, variant := range n.variation.variants {
		variants[index] = UnderlyingVariant{Builds: variant.mask, Underlying: variant.rhs}
	}
	return variants
}

// appendVariant adds variant to variants, folding it into an answer alike.
func appendVariant(variants []objectVariant, variant objectVariant) []objectVariant {
	for index := range variants {
		if sameAnswer(variants[index], variant) {
			variants[index].mask |= variant.mask
			return variants
		}
	}
	return append(variants, variant)
}

// sameAnswer reports whether two answers are alike: identical types, and equal values when they are constants.
func sameAnswer(left objectVariant, right objectVariant) bool {
	if (left.typ == nil) != (right.typ == nil) {
		return false
	}
	if left.typ != nil && !Identical(left.typ, right.typ) {
		return false
	}
	if (left.val == nil) != (right.val == nil) {
		return false
	}
	return left.val == nil || left.val.Kind() == right.val.Kind() && left.val.ExactString() == right.val.ExactString()
}

// splitAt raises a split of the current unit's builds into parts, plus whatever of them no part holds.
func splitAt(parts []Mask) {
	covered := Mask(0)
	for _, part := range parts {
		covered |= part
	}
	if rest := variational.mask &^ covered; rest != 0 {
		parts = append(parts, rest)
	}
	panic(splitRequest{parts: parts})
}

// readingVariation reports whether a read must answer by build: a variational check is in a unit.
func readingVariation() bool {
	return variational.active && variational.depth > 0
}

// pickObjectVariant is the answer of obj's variation for the current unit's builds, splitting the unit when they
// differ.
func pickObjectVariant(variation *objectVariation) (objectVariant, bool) {
	if variation == nil || len(variation.variants) == 0 || !readingVariation() {
		return objectVariant{}, false
	}
	variational.narrowed++
	mask := variational.mask
	first := -1
	var parts []Mask
	for index, variant := range variation.variants {
		if variant.mask&mask == 0 {
			continue
		}
		parts = append(parts, variant.mask&mask)
		if first < 0 {
			first = index
		}
	}
	if len(parts) > 1 {
		splitAt(parts)
	}
	if first < 0 {
		return variation.variants[0], true
	}
	if variation.variants[first].mask&mask != mask {
		// The answer holds in some of the unit's builds and the object is absent in the rest.
		splitAt(parts)
	}
	return variation.variants[first], true
}

// presentHere reports whether an object with variation exists in the current unit's builds: true in all of them,
// false in none, and a split when only some.
func presentHere(variation *objectVariation) bool {
	if variation == nil || variation.presence == 0 || !readingVariation() {
		return true
	}
	mask := variational.mask
	switch {
	case mask&variation.presence == mask:
		return true
	case mask&variation.presence == 0:
		return false
	default:
		splitAt([]Mask{mask & variation.presence, mask &^ variation.presence})
		return false
	}
}

// pickUnderlying is n's underlying type in the current unit's builds, splitting the unit when they differ.
func (n *Named) pickUnderlying() Type {
	variation := n.variation
	if !readingVariation() {
		return variation.variants[0].resolved(n)
	}
	variational.narrowed++
	mask := variational.mask
	first := -1
	var parts []Mask
	for index, variant := range variation.variants {
		if variant.mask&mask == 0 {
			continue
		}
		parts = append(parts, variant.mask&mask)
		if first < 0 {
			first = index
		}
	}
	if len(parts) > 1 {
		splitAt(parts)
	}
	if first < 0 {
		first = 0
	}
	return variation.variants[first].resolved(n)
}

// resolved is variant's underlying type, following its right-hand side through aliases and named types.
func (variant *namedVariant) resolved(n *Named) Type {
	if variant.underlying != nil {
		return variant.underlying
	}
	before := variational.narrowed
	var underlying Type
	switch rhs := Unalias(variant.rhs).(type) {
	case *Named:
		underlying = rhs.Underlying()
	case nil:
		underlying = Typ[Invalid]
	default:
		underlying = rhs.Underlying()
	}
	if variational.narrowed == before {
		variant.underlying = underlying
	}
	return underlying
}

// fileMask is the builds file compiles in.
func (check *Checker) fileMask(file *ast.File) Mask {
	variation := check.conf.Variation
	if variation == nil {
		return 0
	}
	if mask, named := variation.Files[file]; named {
		return mask
	}
	return variation.Builds
}

// vrecord is one Types answer recorded under a unit's builds.
type vrecord struct {
	expr ast.Expr
	mask Mask
	tv   TypeAndValue
	// persistent is set once a declaration unit that recorded it has finished: the declaration never runs again, so
	// an enclosing unit that restarts keeps it.
	persistent bool
	// order is the iota of the constant being declared when it was recorded, or -1. An inherited initializer is one
	// expression written once for each constant of its group, and a plain check's last write is the group's last
	// constant; merged declarations run in another order, so the fold keeps the highest iota instead.
	order int64
}

// runUnit runs run once for builds, or once for each group of them that answers alike when a read inside splits it.
// A declaration unit's work outlives any unit around it that restarts, since a declared object is never declared
// again; an action unit's is redone with the unit around it.
func (check *Checker) runUnit(builds Mask, run func(Mask), declaration bool) {
	pending := []Mask{builds}
	for len(pending) > 0 {
		part := pending[0]
		pending = pending[1:]
		if split := check.tryUnit(part, run, declaration); split != nil {
			pending = append(split, pending...)
		}
	}
}

// tryUnit runs run under builds and returns nil, or the split a read raised, after putting back what the abandoned
// run changed: its records, its delayed actions, its untyped expressions, its object path and its environment. What a
// finished declaration inside it did is kept.
func (check *Checker) tryUnit(builds Mask, run func(Mask), declaration bool) (split []Mask) {
	savedMask := variational.mask
	savedRecords := len(check.vrecords)
	savedDelayed := len(check.delayed)
	savedUntyped := len(check.untypedLog)
	savedPositions := len(check.posStack)
	savedPath := len(check.objPath)
	savedEnvironment := check.environment
	variational.mask = builds
	variational.depth++
	defer func() {
		variational.depth--
		variational.mask = savedMask
		recovered := recover()
		if recovered == nil {
			return
		}
		request, isSplit := recovered.(splitRequest)
		if !isSplit {
			panic(recovered)
		}
		kept := check.vrecords[:savedRecords]
		for _, record := range check.vrecords[savedRecords:] {
			if record.persistent {
				kept = append(kept, record)
			}
		}
		check.vrecords = kept
		for index := savedDelayed; index < len(check.delayed); index++ {
			if !check.delayed[index].persistent {
				check.delayed[index].f = func() {}
			}
		}
		for _, expr := range check.untypedLog[savedUntyped:] {
			delete(check.untyped, expr)
		}
		check.untypedLog = check.untypedLog[:savedUntyped]
		check.posStack = check.posStack[:savedPositions]
		for len(check.objPath) > savedPath {
			check.pop()
		}
		check.environment = savedEnvironment
		variational.variation.Restarts++
		split = request.parts
	}()
	run(builds)
	check.flushUntyped(savedUntyped)
	if declaration {
		for index := savedRecords; index < len(check.vrecords); index++ {
			check.vrecords[index].persistent = true
		}
		for index := savedDelayed; index < len(check.delayed); index++ {
			check.delayed[index].persistent = true
		}
	}
	variational.variation.Units++
	return nil
}

// flushUntyped records, under the current unit's builds, the untyped expressions the unit remembered and never
// typed, since nothing after the unit can type them.
func (check *Checker) flushUntyped(from int) {
	for _, expr := range check.untypedLog[from:] {
		info, remembered := check.untyped[expr]
		if !remembered {
			continue
		}
		delete(check.untyped, expr)
		check.recordTypeAndValue(expr, info.mode, info.typ, info.val)
	}
	check.untypedLog = check.untypedLog[:from]
}

// recordVariational keeps x's answer under the current unit's builds.
func (check *Checker) recordVariational(x ast.Expr, tv TypeAndValue) {
	mask := variational.all
	if variational.depth > 0 {
		mask = variational.mask
	}
	check.vrecords = append(check.vrecords, vrecord{expr: x, mask: mask, tv: tv, order: check.recordOrder})
}

// rewriteVariational replaces the last answer kept for x under the current unit's builds, as recordCommaOkTypes
// rewrites a recorded type.
func (check *Checker) rewriteVariational(x ast.Expr, tv TypeAndValue) {
	for index := len(check.vrecords) - 1; index >= 0; index-- {
		if check.vrecords[index].expr == x {
			check.vrecords[index].tv = tv
			return
		}
	}
}

// startVariational begins a variational check of check's package, if its configuration asks for one.
func (check *Checker) startVariational() bool {
	variation := check.conf.Variation
	if variation == nil {
		return false
	}
	if variation.Types == nil {
		variation.Types = map[ast.Expr][]BuildTypeAndValue{}
	}
	variational.active = true
	variational.all = variation.Builds
	variational.mask = variation.Builds
	variational.depth = 0
	variational.variation = variation
	check.recordOrder = -1
	return true
}

// endVariational writes the kept answers into the variation and ends the check. A later answer for an expression
// replaces an earlier one in the builds both hold, as a plain check's later write replaces its earlier one (an
// inherited iota is written once for each constant of its group, a generic call again once it is instantiated).
func (check *Checker) endVariational() {
	types := variational.variation.Types
	orders := map[ast.Expr][]int64{}
	for _, record := range check.vrecords {
		var answers []BuildTypeAndValue
		var kept []int64
		for index, earlier := range types[record.expr] {
			earlierOrder := orders[record.expr][index]
			if earlierOrder > record.order {
				record.mask &^= earlier.Builds
			} else {
				earlier.Builds &^= record.mask
			}
			if earlier.Builds != 0 {
				answers = append(answers, earlier)
				kept = append(kept, earlierOrder)
			}
		}
		if record.mask == 0 {
			types[record.expr] = answers
			orders[record.expr] = kept
			continue
		}
		folded := false
		for index := range answers {
			if answers[index].TypeAndValue == record.tv {
				answers[index].Builds |= record.mask
				kept[index] = max(kept[index], record.order)
				folded = true
				break
			}
		}
		if !folded {
			answers = append(answers, BuildTypeAndValue{Builds: record.mask, TypeAndValue: record.tv})
			kept = append(kept, record.order)
		}
		types[record.expr] = answers
		orders[record.expr] = kept
	}
	check.vrecords = nil
	variational.active = false
	variational.variation = nil
}

// buildCount is how many builds mask holds.
func buildCount(mask Mask) int { return bits.OnesCount8(uint8(mask)) }

// How mergeVariant declared a package-level object.
const (
	variantNew       = iota // the name's first object: declare it as always
	variantMerged           // folded into the name's first object as another build's declaration
	variantAlternate        // another kind in builds the first doesn't hold: kept beside it, never declared
)

// mergeVariant folds obj, declared by d, into the package-level object of its name when they are declared in disjoint
// builds and are of one kind, so the name is one object with a declaration for each. A name's first object only notes
// the builds it exists in. Objects of one name in overlapping builds are left to be redeclarations, as always.
func (check *Checker) mergeVariant(ident *ast.Ident, obj Object, d *declInfo) int {
	if !variational.active || obj.Name() == "_" {
		return variantNew
	}
	base := obj.(interface{ base() *object }).base()
	first := check.pkg.scope.elems[obj.Name()]
	if first == nil {
		if d.mask != variational.all {
			base.ensureVariation().presence = d.mask
		}
		return variantNew
	}
	firstDecl := check.objMap[first]
	if firstDecl == nil {
		return variantNew
	}
	firstBase := first.(interface{ base() *object }).base()
	firstVariation := firstBase.ensureVariation()
	if len(firstVariation.decls) == 0 {
		firstVariation.decls = []declVariant{{mask: firstDecl.mask, decl: firstDecl, iota: iotaOf(first)}}
		firstVariation.presence = firstDecl.mask
	}
	held := firstVariation.presence
	for _, alternate := range check.pkg.scope.alternates[obj.Name()] {
		held |= variationOf(alternate).presence
	}
	if held&d.mask != 0 {
		return variantNew
	}
	if variantKind(first) != variantKind(obj) {
		base.ensureVariation().presence = d.mask
		if check.pkg.scope.alternates == nil {
			check.pkg.scope.alternates = map[string][]Object{}
		}
		check.pkg.scope.alternates[obj.Name()] = append(check.pkg.scope.alternates[obj.Name()], obj)
		obj.setParent(check.pkg.scope)
		return variantAlternate
	}
	firstVariation.decls = append(firstVariation.decls, declVariant{mask: d.mask, decl: d, iota: iotaOf(obj)})
	firstVariation.presence |= d.mask
	if firstVariation.presence == variational.all {
		firstVariation.presence = 0
	}
	check.recordDef(ident, first)
	return variantMerged
}

// mergeMethodVariant folds method into the method of its name among methods when they are declared in disjoint
// builds, and reports whether it did.
func (check *Checker) mergeMethodVariant(methods []*Func, method *Func) bool {
	if !variational.active {
		return false
	}
	d := check.objMap[method]
	if d == nil {
		return false
	}
	for _, first := range methods {
		if first.name != method.name {
			continue
		}
		firstDecl := check.objMap[first]
		variation := first.ensureVariation()
		if len(variation.decls) == 0 {
			variation.decls = []declVariant{{mask: firstDecl.mask, decl: firstDecl}}
			variation.presence = firstDecl.mask
		}
		if variation.presence&d.mask != 0 {
			return false
		}
		variation.decls = append(variation.decls, declVariant{mask: d.mask, decl: d})
		variation.presence |= d.mask
		if variation.presence == variational.all {
			variation.presence = 0
		}
		delete(check.objMap, method)
		check.recordDef(d.fdecl.Name, first)
		return true
	}
	if d.mask != variational.all {
		method.ensureVariation().presence = d.mask
	}
	return false
}

// iotaOf is a constant's iota as collectObjects left it in the constant's value, or nil.
func iotaOf(obj Object) constant.Value {
	if constantObject, isConstant := obj.(*Const); isConstant {
		return constantObject.val
	}
	return nil
}

// variantKind tells package-level objects' kinds apart.
func variantKind(obj Object) int {
	switch obj.(type) {
	case *Const:
		return 1
	case *Var:
		return 2
	case *TypeName:
		return 3
	case *Func:
		return 4
	}
	return 0
}

// declareVariational checks each of obj's declarations as a unit under the builds its file compiles in, splitting a
// unit whose reads differ across its builds, and keeps obj's answers by build. A named type keeps one identity across
// every run, and its underlying type is what varies.
func (check *Checker) declareVariational(obj Object, d *declInfo) {
	base := obj.(interface{ base() *object }).base()
	variation := variationOf(obj)
	var decls []declVariant
	if variation != nil && len(variation.decls) > 0 {
		decls = variation.decls
	} else {
		mask := d.mask
		if mask == 0 {
			mask = variational.all
		}
		decls = []declVariant{{mask: mask, decl: d, iota: iotaOf(obj)}}
	}
	typeName, isTypeName := obj.(*TypeName)
	isDefinedType := isTypeName && !decls[0].decl.tdecl.Assign.IsValid()
	if isDefinedType || len(decls) > 1 {
		variation = base.ensureVariation()
	}
	var answers []objectVariant
	// An n:1 variable declaration types every variable on its left at once, so each run starts them all untyped and
	// keeps each one's answer.
	var siblings []*Var
	siblingAnswers := map[*Var][]objectVariant{}
	if variable, isVariable := obj.(*Var); isVariable && len(decls) == 1 && len(decls[0].decl.lhs) > 1 {
		for _, sibling := range decls[0].decl.lhs {
			if sibling != variable {
				siblings = append(siblings, sibling)
			}
		}
	}
	every := Mask(0)
	for _, declared := range decls {
		every |= declared.mask
		check.runUnit(declared.mask, func(builds Mask) {
			base.typ = nil
			for _, sibling := range siblings {
				sibling.typ = nil
			}
			if constantObject, isConstant := obj.(*Const); isConstant {
				constantObject.val = declared.iota
			}
			check.environment = environment{scope: declared.decl.file, version: declared.decl.version}
			switch obj := obj.(type) {
			case *Const:
				check.decl = declared.decl
				savedOrder := check.recordOrder
				if iota, isInt := constant.Int64Val(declared.iota); isInt {
					check.recordOrder = iota
				}
				check.constDecl(obj, declared.decl.vtyp, declared.decl.init, declared.decl.inherited)
				check.recordOrder = savedOrder
				answers = append(answers, objectVariant{mask: builds, typ: obj.typ, val: obj.val})
			case *Var:
				check.decl = declared.decl
				check.varDecl(obj, declared.decl.lhs, declared.decl.vtyp, declared.decl.init)
				answers = append(answers, objectVariant{mask: builds, typ: obj.typ})
				for _, sibling := range siblings {
					siblingAnswers[sibling] = append(siblingAnswers[sibling], objectVariant{mask: builds, typ: sibling.typ})
				}
			case *TypeName:
				check.typeDecl(obj, declared.decl.tdecl)
				if named := asNamed(obj.typ); isDefinedType && named != nil {
					answers = append(answers, objectVariant{mask: builds, typ: named.fromRHS})
				} else {
					answers = append(answers, objectVariant{mask: builds, typ: obj.typ})
				}
			case *Func:
				check.funcDecl(obj, declared.decl)
				answers = append(answers, objectVariant{mask: builds, typ: obj.typ})
			default:
				panic("unreachable")
			}
		}, true)
	}
	for _, sibling := range siblings {
		var siblingCollapsed []objectVariant
		for _, answer := range siblingAnswers[sibling] {
			siblingCollapsed = appendVariant(siblingCollapsed, answer)
		}
		if len(siblingCollapsed) == 0 {
			continue
		}
		sibling.typ = siblingCollapsed[0].typ
		if len(siblingCollapsed) > 1 {
			sibling.ensureVariation().variants = siblingCollapsed
		}
	}
	var collapsed []objectVariant
	for _, answer := range answers {
		collapsed = appendVariant(collapsed, answer)
	}
	if isDefinedType {
		named := variation.named
		if named != nil && len(collapsed) > 1 {
			variants := make([]namedVariant, len(collapsed))
			for index, answer := range collapsed {
				variants[index] = namedVariant{mask: answer.mask, rhs: answer.typ}
			}
			named.variation = &namedVariation{variants: variants}
			named.fromRHS = collapsed[0].typ
		}
		if named != nil {
			base.typ = named
		}
		// Methods are collected once, for every build the type is declared in.
		saved := variational.mask
		variational.mask = every
		check.collectMethods(typeName)
		variational.mask = saved
		return
	}
	if len(collapsed) == 0 {
		return
	}
	base.typ = collapsed[0].typ
	if constantObject, isConstant := obj.(*Const); isConstant {
		constantObject.val = collapsed[0].val
	}
	if len(collapsed) > 1 {
		variation = base.ensureVariation()
		variation.variants = collapsed
	}
}

// resetForVariant readies n to be declared again, for another build's declaration or another group of builds after a
// split: its right-hand side, underlying type and type parameters are taken again.
func (n *Named) resetForVariant() {
	n.fromRHS = nil
	n.underlying = nil
	n.tparams = nil
	n.variation = nil
	n.state_ = 0
}

// freshBodyScope puts sig's scope back to what the signature declared in it, when its body runs again: the receiver,
// parameters, results and type parameters, and no child scope the last run opened.
func freshBodyScope(sig *Signature) {
	scope := sig.scope
	if !scope.bodyRan {
		scope.bodyRan = true
		scope.bodyChildren = len(scope.children)
		return
	}
	own := map[Object]bool{}
	if sig.recv != nil {
		own[sig.recv] = true
	}
	for _, tuple := range []*Tuple{sig.params, sig.results} {
		if tuple != nil {
			for _, variable := range tuple.vars {
				own[variable] = true
			}
		}
	}
	for _, list := range []*TypeParamList{sig.tparams, sig.rparams} {
		for _, parameter := range list.list() {
			own[parameter.obj] = true
		}
	}
	for name, obj := range scope.elems {
		if !own[resolve(name, obj)] {
			delete(scope.elems, name)
		}
	}
	scope.children = scope.children[:scope.bodyChildren]
}
