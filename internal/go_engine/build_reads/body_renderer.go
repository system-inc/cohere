package build_reads

import (
	"cmp"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"math/big"
	"slices"
)

// bodyRenderer renders one package's bodies (RenderBodies), on one goroutine, each text kept once by textIndex.
type bodyRenderer struct {
	fileSet    *token.FileSet
	info       *types.Info
	identities *IdentityRenderer
	qualifier  types.Qualifier
	textIndex  map[string]uint32
	renderings *BodyRenderings
	facts      []RenderedFact
}

// body renders the body under roots of the declaration that starts at start and ends at end in tokenFile, when it is
// wanted.
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
				if typed.Value != nil {
					text += " = " + exactConstant(typed.Value)
				}
				renderer.add(tokenFile, expression, false, text)
			}
			if identifier, isIdentifier := expression.(*ast.Ident); isIdentifier {
				if used := renderer.info.Uses[identifier]; used != nil {
					renderer.add(tokenFile, expression, true, renderer.identities.Object(used).String())
				}
			}
			return true
		})
	}
	facts := slices.Clone(renderer.facts)
	slices.SortFunc(facts, func(first, second RenderedFact) int {
		return cmp.Or(cmp.Compare(first.Start, second.Start), cmp.Compare(first.End, second.End),
			compareBool(first.Use, second.Use), cmp.Compare(renderer.renderings.texts[first.Text],
				renderer.renderings.texts[second.Text]))
	})
	renderer.renderings.bodies[place] = RenderedBody{Facts: facts, LastLine: tokenFile.Position(end).Line}
}

// add adds the fact of expression, its use when use is set, rendered as text.
func (renderer *bodyRenderer) add(tokenFile *token.File, expression ast.Expr, use bool, text string) {
	index, isKept := renderer.textIndex[text]
	if !isKept {
		index = uint32(len(renderer.renderings.texts))
		renderer.textIndex[text] = index
		renderer.renderings.texts = append(renderer.renderings.texts, text)
	}
	renderer.facts = append(renderer.facts, RenderedFact{Start: int32(tokenFile.Offset(expression.Pos())),
		End: int32(tokenFile.Offset(expression.End())), Use: use, Text: index})
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
