package build_reads

import (
	"cmp"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"math/big"
	"slices"
	"strconv"
)

// bodyRenderer renders one package's bodies (RenderBodies), own's, on one goroutine, each text kept once by
// textIndex, and with namedTypes the named types each expression's type names written beside it.
type bodyRenderer struct {
	fileSet    *token.FileSet
	own        *types.Package
	info       *types.Info
	identities *IdentityRenderer
	namedTypes func(types.Type) string
	qualifier  types.Qualifier
	textIndex  map[string]uint32
	renderings *BodyRenderings
	facts      []RenderedFact
}

// body renders the body under roots of the declaration that starts at start and ends at end in tokenFile, when it is
// wanted. A use of an object the declaration declares itself, a local, a parameter or a label, is written by its
// name and its offset in the file, which both builds share, as stage 1's record leaves it out (declaredWithin); any
// other use whose identity is unknown is marked so, since it can match no other (Identity.Equal).
func (renderer *bodyRenderer) body(
	tokenFile *token.File,
	start token.Pos,
	end token.Pos,
	wanted func(BodyPlace) bool,
	roots ...ast.Node,
) {
	place := BodyPlace{File: tokenFile.Position(start).Filename, Offset: tokenFile.Offset(start)}
	if wanted != nil && !wanted(place) {
		return
	}
	renderer.facts = renderer.facts[:0]
	for _, root := range roots {
		ast.Inspect(root, func(node ast.Node) bool {
			expression, isExpression := node.(ast.Expr)
			if !isExpression {
				return true
			}
			if typed, isTyped := renderer.info.Types[expression]; isTyped && typed.Type != nil {
				text := types.TypeString(typed.Type, renderer.qualifier)
				if renderer.namedTypes != nil {
					text += renderer.namedTypes(typed.Type)
				}
				if typed.Value != nil {
					text += " = " + exactConstant(typed.Value)
				}
				renderer.add(tokenFile, expression, false, false, text)
			}
			if identifier, isIdentifier := expression.(*ast.Ident); isIdentifier {
				used := renderer.info.Uses[identifier]
				switch {
				case used == nil:
				case declaredWithin(used, renderer.own, start, end):
					renderer.add(tokenFile, expression, true, false,
						"own "+used.Name()+" at "+strconv.Itoa(tokenFile.Offset(used.Pos())))
				default:
					identity := renderer.identities.Object(used)
					renderer.add(tokenFile, expression, true, identity.Kind == IdentityUnknown, identity.String())
				}
			}
			return true
		})
	}
	facts := slices.Clone(renderer.facts)
	slices.SortFunc(facts, func(first, second RenderedFact) int {
		return cmp.Or(cmp.Compare(first.Start, second.Start), cmp.Compare(first.End, second.End),
			compareBool(first.Use, second.Use), compareBool(first.Unknown, second.Unknown),
			cmp.Compare(renderer.renderings.texts[first.Text], renderer.renderings.texts[second.Text]))
	})
	renderer.renderings.bodies[place] = RenderedBody{Facts: facts, LastLine: tokenFile.Position(end).Line}
}

// add adds the fact of expression, its use when use is set, of an unknown identity when unknown is, rendered as text.
func (renderer *bodyRenderer) add(tokenFile *token.File, expression ast.Expr, use bool, unknown bool, text string) {
	index, isKept := renderer.textIndex[text]
	if !isKept {
		index = uint32(len(renderer.renderings.texts))
		renderer.textIndex[text] = index
		renderer.renderings.texts = append(renderer.renderings.texts, text)
	}
	renderer.facts = append(renderer.facts, RenderedFact{Start: int32(tokenFile.Offset(expression.Pos())),
		End: int32(tokenFile.Offset(expression.End())), Use: use, Unknown: unknown, Text: index})
}

// exactConstant is value written exactly and one way only: a float or a complex part as its exact fraction, whichever
// representation go/constant holds it in, since a constant read from export data holds a float as a binary fraction
// where one checked from source holds it as a ratio, and the two write the same number apart.
func exactConstant(value constant.Value) string {
	switch value.Kind() {
	case constant.Float:
		switch exact := constant.Val(value).(type) {
		case *big.Rat:
			return exact.RatString()
		case *big.Float:
			if rational, _ := exact.Rat(nil); rational != nil {
				return rational.RatString()
			}
		}
	case constant.Complex:
		return "(" + exactConstant(constant.Real(value)) + " + " + exactConstant(constant.Imag(value)) + "i)"
	}
	return value.ExactString()
}
