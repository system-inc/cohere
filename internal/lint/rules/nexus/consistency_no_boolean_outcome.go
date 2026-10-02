package nexus

import (
	"regexp"
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/ecmascript/imports"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// outcomeFlagNames claim to say how an operation turned out.
//
// `ok` belongs here despite also being the fetch Response field, because the rule fires on
// declaration sites and never on a property read.
var outcomeFlagNames = map[string]bool{
	"success": true, "succeeded": true, "ok": true, "isSuccess": true,
	"isOk": true, "failed": true, "isError": true, "isFailure": true,
}

// resultCompanionNames turn a boolean flag into a result envelope rather than ordinary state.
var resultCompanionNames = map[string]bool{
	"error": true, "errors": true, "message": true, "value": true,
	"result": true, "reason": true, "data": true,
}

// thirdPartyRequestFieldNames are TanStack Query's own fields. A declaration carrying one beside its
// flag is mirroring a third-party result rather than declaring one, so the naming is not ours.
var thirdPartyRequestFieldNames = map[string]bool{
	"isLoading": true, "isPending": true, "isFetching": true, "isRefetching": true, "status": true,
}

// roleSuffixes are stripped from a declaration name so the suggestion reads right:
// ClaudeCallResultInterface suggests ClaudeCall.
var roleSuffixes = regexp.MustCompile(`(Interface|Type|Result|Response|Properties)+$`)

// ConsistencyNoBooleanOutcomeOptions exempts declarations by exact name.
type ConsistencyNoBooleanOutcomeOptions struct {
	AllowedTypeNames []string
}

func messageBooleanOutcome(flagName string, declaration string, suggested string) rule.Message {
	return rule.Message{
		Id: "booleanOutcome",
		Description: "`" + flagName + ": boolean` on " + declaration + " collapses every outcome into one " +
			"bit, at the moment the distinction is cheapest to keep. Return a named outcome instead: a " +
			"union of `outcome` members naming each way the operation can turn out, with the payload on " +
			"the arm that carries it. Suggested name: " + suggested + "OutcomeType.",
	}
}

// ConsistencyNoBooleanOutcome flags a result shape that reports how an operation turned out with a
// boolean flag paired with an error or value field.
//
//	valid:   type PostOutcomeType = { outcome: 'Found'; value: PostView } | { outcome: 'NotFound' }
//	valid:   interface ToggleState { isVisible: boolean }
//	invalid: interface LookupResult { success: boolean; error: string }
//
// A boolean collapses every way an operation can turn out into one bit. "Not shaped like a code at
// all" and "shaped like one, but no such code exists" are different facts a caller usually wants to
// act on. A named member set also stops framing ordinary answers as failures: a lookup that finds
// nothing is an answer, not a fault.
//
// The exemptions are what make it usable, and each is structural rather than a name guess. A flag
// with no companion field is ordinary state, so a bare isVisible or a lone succeeded column is left
// alone. React properties describe what to render, so an isError there is display state the parent
// hands down. A declaration carrying TanStack's field set is mirroring someone else's result.
// isSuccess and isError together describe one settled state from two sides, which is that same
// third-party shape rather than an envelope. Generated output belongs to its generator.
//
// No fix. Naming the ways an operation can turn out is the judgment the pattern exists for.
var ConsistencyNoBooleanOutcome = rule.Rule{
	Name: "nexus/consistency-no-boolean-outcome",
	Run: func(ctx rule.Context, options any) rule.Listeners {
		if ctx.SourceFile == nil {
			return nil
		}

		filePath := imports.NormalizedFileName(ctx.SourceFile)
		if strings.Contains(filePath, "/generated/") || strings.Contains(filePath, ".generated.") {
			return nil
		}

		allowedTypeNames := map[string]bool{}
		if settings, hasSettings := rule.OptionsAs[ConsistencyNoBooleanOutcomeOptions](options); hasSettings {
			for _, name := range settings.AllowedTypeNames {
				allowedTypeNames[name] = true
			}
		}

		report := func(members []*ast.Node, declarationName string, declarationKind string) {
			if allowedTypeNames[declarationName] {
				return
			}
			// React props describe what to render, not how an operation turned out.
			if strings.HasSuffix(declarationName, "Properties") {
				return
			}

			flag := findBooleanOutcomeFlag(members)
			if flag == nil {
				return
			}

			suggested := roleSuffixes.ReplaceAllString(declarationName, "")
			if suggested == "" {
				suggested = declarationName
			}
			ctx.ReportNode(flag, messageBooleanOutcome(
				propertySignatureName(flag), declarationKind+" "+declarationName, suggested))
		}

		return rule.Listeners{
			ast.KindInterfaceDeclaration: func(node *ast.Node) {
				declaration := node.AsInterfaceDeclaration()
				if declaration == nil || declaration.Name() == nil || declaration.Members == nil {
					return
				}
				report(declaration.Members.Nodes, declaration.Name().Text(), "interface")
			},

			ast.KindTypeAliasDeclaration: func(node *ast.Node) {
				declaration := node.AsTypeAliasDeclaration()
				if declaration == nil || declaration.Name() == nil || declaration.Type == nil {
					return
				}
				// Only a bare type literal is a struct. A union alias is already discriminated and
				// is the shape this rule steers toward.
				if declaration.Type.Kind != ast.KindTypeLiteral {
					return
				}
				literal := declaration.Type.AsTypeLiteralNode()
				if literal == nil || literal.Members == nil {
					return
				}
				report(literal.Members.Nodes, declaration.Name().Text(), "type")
			},
		}
	},
}

// propertySignatureName returns the identifier a property signature declares, or empty.
func propertySignatureName(member *ast.Node) string {
	if member == nil || member.Kind != ast.KindPropertySignature {
		return ""
	}
	signature := member.AsPropertySignatureDeclaration()
	if signature == nil {
		return ""
	}
	name := signature.Name()
	if name == nil || name.Kind != ast.KindIdentifier {
		return ""
	}
	return name.Text()
}

// findBooleanOutcomeFlag returns the flag property when a member list is an envelope: a boolean
// outcome flag plus a companion field, with none of the exemptions applying.
func findBooleanOutcomeFlag(members []*ast.Node) *ast.Node {
	// Plain identifier keys only, so a computed or string-literal key is skipped.
	properties := map[string]*ast.Node{}
	order := []string{}
	for _, member := range members {
		name := propertySignatureName(member)
		if name == "" {
			continue
		}
		if _, seen := properties[name]; !seen {
			order = append(order, name)
		}
		properties[name] = member
	}

	// Iterate declaration order rather than map order, so the reported flag is stable across runs.
	var flag *ast.Node
	flagName := ""
	for _, name := range order {
		if !outcomeFlagNames[name] {
			continue
		}
		// A literal `success: true` arm inside an already-discriminated union is fine, so only a
		// bare boolean counts.
		signature := properties[name].AsPropertySignatureDeclaration()
		if signature == nil || signature.Type == nil || signature.Type.Kind != ast.KindBooleanKeyword {
			continue
		}
		flag = properties[name]
		flagName = name
		break
	}
	if flag == nil {
		return nil
	}

	// A flag on its own is state, not a result. Require a companion.
	hasCompanion := false
	for _, name := range order {
		if name != flagName && resultCompanionNames[name] {
			hasCompanion = true
			break
		}
	}
	if !hasCompanion {
		return nil
	}

	// Mirroring a third-party request result rather than declaring our own.
	for _, name := range order {
		if thirdPartyRequestFieldNames[name] {
			return nil
		}
	}

	// isSuccess and isError together describe one settled state from two sides, which is that same
	// third-party shape rather than an envelope.
	_, hasIsSuccess := properties["isSuccess"]
	_, hasIsError := properties["isError"]
	if hasIsSuccess && hasIsError {
		return nil
	}

	return flag
}
