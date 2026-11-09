// Package consistency_no_boolean_outcome is the Go engine's rule that a struct does not say how an operation turned
// out with a bool beside an error or a value: type LookupResult struct { Success bool; Error error }.
//
// One bit collapses every way the operation can turn out, at the moment the distinction is cheapest to keep, and it
// leaves the reader to know which other fields hold for which value of the flag: is Value the answer when Ok is false,
// a zero, or a leftover? A lookup that finds nothing is an answer, not a fault, and "not shaped like a code" and
// "shaped like one, but no such code exists" are different facts a caller usually wants to act on. Go already has the
// repair in its idiom: a function returns (value, error), and an outcome with more than two ways to go gets a named
// type with a constant for each.
//
// This is the Go form of nexus/consistency-no-boolean-outcome, which reads an interface and a type alias of a bare
// object literal, and its Swift port, which reads a struct and a typealias of a labeled tuple. The shapes map one to
// one:
//   - A declared struct type, type T struct { ... } or type T = struct { ... }, at package scope or in a function, is
//     the named field set. Its fields are the ones it declares, an embedded field by the name it is reached by; a field
//     promoted from an embedded struct is that struct's, as an interface's inherited members are not read.
//   - A flag is a field named one of the twins' names (success, succeeded, ok, isSuccess, isOk, failed, isError,
//     isFailure), up to the case of its first letter, so Ok and ok both count, whose type is the predeclared bool. A
//     *bool, a named type whose underlying type is bool, and a type parameter are not the bare two-valued flag, as the
//     twins skip an optional or literal flag. A companion is a field named error, errors, message, value, result,
//     reason or data, the same way, of any type. The first flag in declaration order is the one reported.
//   - The twins' exemptions are kept as they are, so the three languages decide the same field set the same way: a
//     flag with no companion is ordinary state; a field set carrying any of TanStack Query's own fields (isLoading,
//     isPending, isFetching, isRefetching, status) or every field of Cloudflare API v4's envelope (success, errors,
//     messages, result) mirrors a third party's result, and a status beside ok mirrors an HTTP reply in Go as well;
//     isSuccess beside isError is one settled state seen from two sides; a name ending Properties is display state.
//
// What it leaves alone, and why: a function's (value, ok) or (value, error) results, which are Go's idiom for this and
// not a field set; an interface's methods; and a struct type spelled inline, in a var, a composite literal, a slice's
// element or a function's result, as the twins leave an inline object type or tuple alone, since none of them is a
// declaration a reader looks the shape up by. Go spells the acronym OK, which the twins' list does not hold and the
// house spells Ok, so a field named OK is a miss. A type parenthesized in its declaration is not the bare literal and
// is not read, as in both twins. The TypeScript rule's allowedTypeNames option has no counterpart, as in the Swift
// port.
//
// There is no fix: naming the ways an operation can turn out is the judgment the rule exists for.
package consistency_no_boolean_outcome

import (
	"go/ast"
	"go/token"
	"go/types"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/consistency-no-boolean-outcome"

// listener hears every type spec of the syntax dispatcher's walk.
var listener = rule.Listener{
	FileLocal: true, Nodes: []ast.Node{(*ast.TypeSpec)(nil)}, Node: judgeTypeSpec}

// Analyzer runs the rule over one package, reporting what the syntax dispatcher's walk found for it.
var Analyzer = dispatch.RuleAnalyzer(Name, "consistency_no_boolean_outcome", "Flags a declared struct type with a "+
	"bool field named success, succeeded, ok, isSuccess, isOk, failed, isError or isFailure beside a field named "+
	"error, errors, message, value, result, reason or data, the result envelope that says how an operation turned out "+
	"in one bit. A function's (value, ok) and (value, error) results are Go's idiom and are not read, nor is a struct "+
	"spelled inline.", &listener)

// Rule is this rule's entry in the Go engine's registry.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: true, Analyzer: Analyzer, Listener: &listener}

var booleanOutcomeMessage = policy.MessageOf(Name, "booleanOutcome")

// outcomeFlagNames claim to say how an operation turned out, the twins' list, compared with a field's first letter
// lowered.
var outcomeFlagNames = map[string]bool{
	"success": true, "succeeded": true, "ok": true, "isSuccess": true,
	"isOk": true, "failed": true, "isError": true, "isFailure": true,
}

// resultCompanionNames turn a flag into a result envelope rather than ordinary state.
var resultCompanionNames = map[string]bool{
	"error": true, "errors": true, "message": true, "value": true,
	"result": true, "reason": true, "data": true,
}

// thirdPartyRequestFieldNames are TanStack Query's own fields. A field set carrying one beside its flag mirrors a
// third party's request result rather than declaring one, so the naming is not ours.
var thirdPartyRequestFieldNames = map[string]bool{
	"isLoading": true, "isPending": true, "isFetching": true, "isRefetching": true, "status": true,
}

// thirdPartyEnvelopes are result envelopes another system defines and sends over the wire, each the full field set
// its documentation gives. The whole set is required, so a shape of our own that shares a field or two with one is
// still reported.
var thirdPartyEnvelopes = [][]string{
	// Every Cloudflare API v4 response, success or failure:
	// https://developers.cloudflare.com/email-service/api/send-emails/rest-api/
	{"success", "errors", "messages", "result"},
}

// roleSuffixes are stripped from a declaration's name so the suggestion reads right: LookupResult suggests
// LookupOutcome.
var roleSuffixes = regexp.MustCompile(`(Interface|Type|Result|Response|Properties)+$`)

// field is one field a struct declares.
type field struct {
	// name is the field's name as declared, Ok, which the message quotes.
	name string
	// key is the name with its first letter lowered, ok, as the twins' lists spell it.
	key string
	// isBool is whether the field's type is the predeclared bool.
	isBool bool
	// start and end are the span a finding on the field covers: a named field's own name through its type, so
	// Ready, Ok bool marks Ok bool, and an embedded field's type.
	start token.Pos
	end   token.Pos
}

// judgeTypeSpec reports the first outcome flag of a type spec that declares a bare struct literal.
func judgeTypeSpec(context *rule.Context, node ast.Node, _ []ast.Node) {
	typeSpec, isTypeSpec := node.(*ast.TypeSpec)
	if !isTypeSpec {
		return
	}
	// Only the bare struct literal is a named field set, as only a bare object literal is in TypeScript.
	structType, isStruct := typeSpec.Type.(*ast.StructType)
	if !isStruct {
		return
	}
	// React properties describe what to render, not how an operation turned out, and the twins exempt the name
	// everywhere, so the three languages agree on it.
	if strings.HasSuffix(typeSpec.Name.Name, "Properties") {
		return
	}
	fields := structFields(context, structType)
	flag := booleanOutcomeFlag(fields)
	if flag == nil {
		return
	}
	suggested := roleSuffixes.ReplaceAllString(typeSpec.Name.Name, "")
	if suggested == "" {
		suggested = typeSpec.Name.Name
	}
	context.Report(analysis.Diagnostic{
		Pos:      flag.start,
		End:      flag.end,
		Category: booleanOutcomeMessage.Id,
		Message: booleanOutcomeMessage.Render(map[string]string{
			"flagName":    flag.name,
			"declaration": "struct " + typeSpec.Name.Name,
			"suggested":   suggested,
		}),
	})
}

// structFields is every field structType declares, in declaration order, each with its type read from the type
// checker so that bool is the predeclared type and not a spelling. It is nil when the checker has no struct for the
// literal, which only broken code gives.
func structFields(context *rule.Context, structType *ast.StructType) []field {
	checked, isStruct := context.TypesInfo.TypeOf(structType).(*types.Struct)
	if !isStruct {
		return nil
	}
	// The checker holds one field for each name, and one for an embedded field, in the order the source declares
	// them, so the syntax's fields are walked beside it by index. A count that does not agree is broken code.
	var fields []field
	index := 0
	for _, declared := range structType.Fields.List {
		// An embedded field has no name of its own in the syntax, so a finding on it starts at its type.
		starts := []token.Pos{declared.Type.Pos()}
		if len(declared.Names) > 0 {
			starts = nil
			for _, name := range declared.Names {
				starts = append(starts, name.Pos())
			}
		}
		for _, start := range starts {
			if index >= checked.NumFields() {
				return nil
			}
			variable := checked.Field(index)
			index++
			basic, isBasic := types.Unalias(variable.Type()).(*types.Basic)
			fields = append(fields, field{
				name:   variable.Name(),
				key:    lowerFirst(variable.Name()),
				isBool: isBasic && basic.Kind() == types.Bool,
				start:  start,
				end:    declared.Type.End(),
			})
		}
	}
	if index != checked.NumFields() {
		return nil
	}
	return fields
}

// booleanOutcomeFlag is the flag when fields are a result envelope: a bool outcome flag and a companion, with none of
// the exemptions applying. Otherwise it is nil.
func booleanOutcomeFlag(fields []field) *field {
	keys := map[string]bool{}
	for _, candidate := range fields {
		keys[candidate.key] = true
	}
	var flag *field
	for index := range fields {
		if outcomeFlagNames[fields[index].key] && fields[index].isBool {
			flag = &fields[index]
			break
		}
	}
	if flag == nil {
		return nil
	}
	// A flag on its own is state, not a result, so a companion is required.
	hasCompanion := false
	for key := range resultCompanionNames {
		hasCompanion = hasCompanion || keys[key]
	}
	if !hasCompanion {
		return nil
	}
	// Mirroring a third party's request result rather than declaring our own.
	for key := range thirdPartyRequestFieldNames {
		if keys[key] {
			return nil
		}
	}
	// Mirroring a third party's wire envelope, every one of its documented fields.
	for _, envelope := range thirdPartyEnvelopes {
		carriesAll := true
		for _, key := range envelope {
			carriesAll = carriesAll && keys[key]
		}
		if carriesAll {
			return nil
		}
	}
	// isSuccess and isError together describe one settled state from two sides, a third party's shape rather than an
	// envelope.
	if keys["isSuccess"] && keys["isError"] {
		return nil
	}
	return flag
}

// lowerFirst is name with its first letter lowered, so an exported field, Ok, reads as the twins' lists spell it.
func lowerFirst(name string) string {
	first, size := utf8.DecodeRuneInString(name)
	return string(unicode.ToLower(first)) + name[size:]
}
