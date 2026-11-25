package type_checking

import (
	"slices"
	"strconv"
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/checker"
)

type stableTypePrinter struct {
	typeChecker *checker.Checker

	// maximumMembers and maximumDepth are this printing's limits. See stableTypeLimits.
	maximumMembers int
	maximumDepth   int
	// stack is the types being printed, so a type that contains itself prints by name the second time.
	stack []*checker.Type
}

func (printer *stableTypePrinter) print(subjectType *checker.Type, depth int) string {
	if subjectType == nil {
		return "unknown"
	}
	if alias := subjectType.Alias(); alias != nil && alias.Symbol() != nil {
		return printer.named(alias.Symbol().Name, alias.TypeArguments(), depth)
	}
	switch {
	case subjectType.Flags()&checker.TypeFlagsUnion != 0:
		return printer.union(subjectType, depth)
	case subjectType.Flags()&checker.TypeFlagsIntersection != 0:
		return printer.joined(subjectType.Types(), " & ", depth)
	case subjectType.Flags()&checker.TypeFlagsObject == 0:
		// Primitives, literals, enums, type parameters and the rest print a name of their own, which no other
		// type's order reaches.
		return printer.typeChecker.TypeToString(subjectType)
	}
	if slices.Contains(printer.stack, subjectType) || depth > printer.maximumDepth {
		if symbol := subjectType.Symbol(); symbol != nil &&
			subjectType.ObjectFlags()&checker.ObjectFlagsAnonymous == 0 {
			return symbol.Name
		}
		return "..."
	}
	printer.stack = append(printer.stack, subjectType)
	defer func() { printer.stack = printer.stack[:len(printer.stack)-1] }()

	if checker.Checker_isArrayType(printer.typeChecker, subjectType) {
		arguments := printer.typeChecker.GetTypeArguments(subjectType)
		element := "unknown"
		if len(arguments) > 0 {
			element = printer.operand(arguments[0], depth+1)
		}
		if subjectType.Target() != nil &&
			subjectType.Target().Symbol() != nil &&
			subjectType.Target().Symbol().Name == "ReadonlyArray" {
			return "readonly " + element + "[]"
		}
		return element + "[]"
	}
	if checker.IsTupleType(subjectType) {
		return printer.tuple(subjectType, depth)
	}
	if subjectType.ObjectFlags()&checker.ObjectFlagsReference != 0 &&
		subjectType.Target() != nil &&
		subjectType.Target().Symbol() != nil {
		arguments := printer.typeChecker.GetTypeArguments(subjectType)
		if target := subjectType.Target().TargetInterfaceType(); target != nil &&
			len(target.TypeParameters()) <= len(arguments) {
			arguments = arguments[:len(target.TypeParameters())]
		}
		return printer.named(subjectType.Target().Symbol().Name, arguments, depth)
	}
	if subjectType.ObjectFlags()&checker.ObjectFlagsAnonymous == 0 && subjectType.Symbol() != nil {
		return printer.typeChecker.TypeToString(subjectType)
	}
	return printer.anonymous(subjectType, depth)
}

// named prints a name with its type arguments, each by these rules.
func (printer *stableTypePrinter) named(name string, arguments []*checker.Type, depth int) string {
	if len(arguments) == 0 {
		return name
	}
	printed := make([]string, len(arguments))
	for index, argument := range arguments {
		printed[index] = printer.print(argument, depth+1)
	}
	return name + "<" + strings.Join(printed, ", ") + ">"
}

// operand is a type printed where an operator follows it, `T[]`: a union, an intersection or a function is
// parenthesized, as TypeScript prints it.
func (printer *stableTypePrinter) operand(subjectType *checker.Type, depth int) string {
	text := printer.print(subjectType, depth)
	if subjectType.Alias() == nil &&
		(subjectType.Flags()&(checker.TypeFlagsUnion|checker.TypeFlagsIntersection) != 0 ||
			strings.Contains(text, "=>")) {
		return "(" + text + ")"
	}
	return text
}

// union prints a union's members in their stable order. `true | false` is `boolean`, as TypeScript prints it.
func (printer *stableTypePrinter) union(subjectType *checker.Type, depth int) string {
	var printed []string
	var booleans []*checker.Type
	for _, member := range subjectType.Types() {
		if member.Flags()&checker.TypeFlagsBooleanLiteral != 0 {
			booleans = append(booleans, member)
			continue
		}
		printed = append(printed, printer.print(member, depth+1))
	}
	switch len(booleans) {
	case 2:
		printed = append(printed, "boolean")
	case 1:
		printed = append(printed, printer.typeChecker.TypeToString(booleans[0]))
	}
	return printer.sortedJoin(printed, " | ")
}

// joined prints members each by these rules and joins them in their stable order.
func (printer *stableTypePrinter) joined(members []*checker.Type, separator string, depth int) string {
	printed := make([]string, len(members))
	for index, member := range members {
		if separator == " & " {
			printed[index] = printer.operand(member, depth+1)
		} else {
			printed[index] = printer.print(member, depth+1)
		}
	}
	return printer.sortedJoin(printed, separator)
}

// sortedJoin orders printed members by their text, with null and undefined last, and keeps the first
// maximumMembers of them.
func (printer *stableTypePrinter) sortedJoin(printed []string, separator string) string {
	rank := func(text string) int {
		switch text {
		case "null":
			return 1
		case "undefined":
			return 2
		}
		return 0
	}
	slices.SortStableFunc(printed, func(left, right string) int {
		if rank(left) != rank(right) {
			return rank(left) - rank(right)
		}
		return strings.Compare(left, right)
	})
	shown := printed
	if len(printed) > printer.maximumMembers {
		more := len(printed) - printer.maximumMembers + 1
		shown = append(printed[:printer.maximumMembers-1], "... "+strconv.Itoa(more)+" more ...")
	}
	return strings.Join(shown, separator)
}

// tuple prints a tuple's elements in their positions, which no checker reorders.
func (printer *stableTypePrinter) tuple(subjectType *checker.Type, depth int) string {
	arguments := printer.typeChecker.GetTypeArguments(subjectType)
	tuple := subjectType.TargetTupleType()
	flags := tuple.ElementFlags()
	printed := make([]string, 0, len(flags))
	for index := range flags {
		if index >= len(arguments) {
			break
		}
		element := printer.print(arguments[index], depth+1)
		switch {
		case flags[index]&checker.ElementFlagsOptional != 0:
			element = printer.operand(arguments[index], depth+1) + "?"
		case flags[index]&checker.ElementFlagsVariable != 0:
			element = "..." + element
		}
		printed = append(printed, element)
	}
	text := "[" + strings.Join(printed, ", ") + "]"
	if tuple.IsReadonly() {
		return "readonly " + text
	}
	return text
}

// anonymous prints an object type that has no name: a function as `(p: T) => R`, and anything else as its
// signatures, index signatures and members in braces.
func (printer *stableTypePrinter) anonymous(subjectType *checker.Type, depth int) string {
	calls := printer.typeChecker.GetSignaturesOfType(subjectType, checker.SignatureKindCall)
	constructs := printer.typeChecker.GetSignaturesOfType(subjectType, checker.SignatureKindConstruct)
	indexes := printer.typeChecker.GetIndexInfosOfType(subjectType)
	properties := printer.members(subjectType)
	if len(calls) == 1 && len(constructs) == 0 && len(indexes) == 0 && len(properties) == 0 {
		return printer.signature(calls[0], " => ", depth)
	}
	if len(constructs) == 1 && len(calls) == 0 && len(indexes) == 0 && len(properties) == 0 {
		return "new " + printer.signature(constructs[0], " => ", depth)
	}
	var parts []string
	for _, call := range calls {
		parts = append(parts, printer.signature(call, ": ", depth))
	}
	for _, construct := range constructs {
		parts = append(parts, "new "+printer.signature(construct, ": ", depth))
	}
	for _, index := range indexes {
		part := "[key: " + printer.print(index.KeyType(), depth+1) + "]: " + printer.print(index.ValueType(), depth+1)
		if index.IsReadonly() {
			part = "readonly " + part
		}
		parts = append(parts, part)
	}
	for _, property := range properties {
		parts = append(parts, printer.member(property, depth))
	}
	if len(parts) == 0 {
		return "{}"
	}
	if len(parts) > printer.maximumMembers {
		more := len(parts) - printer.maximumMembers + 1
		parts = append(parts[:printer.maximumMembers-1], "... "+strconv.Itoa(more)+" more ...")
	}
	return "{ " + strings.Join(parts, "; ") + "; }"
}

// members is an anonymous object's properties in their stable order: its own, declared directly in the declaration
// it came from, by position, then every other one by name. The others are the `name?: undefined` members that
// normalization adds, whose declaration is a sibling's, and which sibling is the checker's history.
//
// Own is where the declaration sits, not its place in the source (#ncz8caa). A borrowed sibling can sit in a literal
// nested inside this one, inside its range but not among its members, and then whether it read as own was the
// checker's history again, as a spread of a nested literal's member was on www-phi-health. memberHome is exact for
// every container measured there: object literals, type literals, a class's static side, namespaces and files.
func (printer *stableTypePrinter) members(subjectType *checker.Type) []*ast.Symbol {
	properties := slices.Clone(printer.typeChecker.GetPropertiesOfType(subjectType))
	var container *ast.Node
	if symbol := subjectType.Symbol(); symbol != nil && len(symbol.Declarations) > 0 {
		container = symbol.Declarations[0]
	}
	own := func(property *ast.Symbol) (int, bool) {
		if container == nil || len(property.Declarations) == 0 || memberHome(property.Declarations[0]) != container {
			return 0, false
		}
		return property.Declarations[0].Pos(), true
	}
	slices.SortStableFunc(properties, func(left, right *ast.Symbol) int {
		positionA, ownA := own(left)
		positionB, ownB := own(right)
		switch {
		case ownA && ownB:
			return positionA - positionB
		case ownA:
			return -1
		case ownB:
			return 1
		}
		return strings.Compare(left.Name, right.Name)
	})
	return properties
}

// member prints one property: `readonly name?: T`, or a method as `name(p: T): R`.
//
// An optional member's type is the one TypeScript's own printer starts from, without the missing type the `?` already
// says (#sp4xwtj). Under exactOptionalPropertyTypes that is only the marker, so `y?: number` prints as written and a
// written `y?: number | undefined` keeps its undefined, and a synthesized `y?:` member prints `never`, as TypeToString
// prints it. Without the option the marker is undefined itself, `y?: number` and `y?: number | undefined` are one
// type, and it prints `| undefined`: TypeToString drops it only where it reuses a written annotation.
func (printer *stableTypePrinter) member(property *ast.Symbol, depth int) string {
	name := StablePropertyName(property.Name)
	if property.Flags&ast.SymbolFlagsOptional != 0 {
		name += "?"
	}
	propertyType := checker.Checker_getNonMissingTypeOfSymbol(printer.typeChecker, property)
	if property.Flags&ast.SymbolFlagsMethod != 0 {
		if signatures := printer.typeChecker.GetSignaturesOfType(
			propertyType,
			checker.SignatureKindCall,
		); len(signatures) == 1 {
			return name + printer.signature(signatures[0], ": ", depth)
		}
	}
	text := name + ": " + printer.print(propertyType, depth+1)
	if checker.Checker_isReadonlySymbol(printer.typeChecker, property) {
		text = "readonly " + text
	}
	return text
}

// signature prints `<T>(a: A, b?: B, ...rest: C[])` followed by the arrow or colon and the return type.
func (printer *stableTypePrinter) signature(signature *checker.Signature, arrow string, depth int) string {
	var text strings.Builder
	if typeParameters := signature.TypeParameters(); len(typeParameters) > 0 {
		names := make([]string, len(typeParameters))
		for index, typeParameter := range typeParameters {
			names[index] = printer.typeChecker.TypeToString(typeParameter)
		}
		text.WriteString("<" + strings.Join(names, ", ") + ">")
	}
	parameters := signature.Parameters()
	printed := make([]string, len(parameters))
	for index, parameter := range parameters {
		prefix, suffix := "", ""
		switch {
		case signature.HasRestParameter() && index == len(parameters)-1:
			prefix = "..."
		case index >= signature.MinArgumentCount():
			suffix = "?"
		}
		printed[index] = prefix +
			parameter.Name +
			suffix +
			": " +
			printer.print(printer.typeChecker.GetTypeOfSymbol(parameter), depth+1)
	}
	text.WriteString("(" + strings.Join(printed, ", ") + ")")
	text.WriteString(arrow + printer.print(printer.typeChecker.GetReturnTypeOfSignature(signature), depth+1))
	return text.String()
}
