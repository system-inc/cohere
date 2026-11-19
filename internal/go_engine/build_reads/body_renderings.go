package build_reads

import (
	"go/ast"
	"go/token"
	"go/types"
)

// BodyRenderings are how one package's check typed each of its bodies, written out, for the body differential of
// stage 2 (#w72zxtq): a body the reach leaves unreached in an extra build must type-check there as it did on the
// host, so the host renders every body as it records its reads, and a build renders the bodies the reach would skip,
// and the two are compared. A body is a function's block or a var or const spec's values, placed by its declaration
// as stage 1's record places it (Declaration.Offset), and rendered as its facts: every expression's type, and its
// constant value when it has one, and every identifier's use, by its stable identity (IdentityRenderer), each at its
// offsets in the file. A function's body takes its receiver and signature with its block, and a value spec's its
// declared type with its values, so a body reached by its signature alone has something to compare. Each text is held
// once, so a package's renderings cost a few words a fact.
type BodyRenderings struct {
	texts  []string
	bodies map[BodyPlace]RenderedBody
}

// RenderedBody is one body rendered: its facts, sorted, and the line its body ends on, which with its declaration's
// first line bounds the findings compared with it.
type RenderedBody struct {
	Facts    []RenderedFact
	LastLine int
}

// BodyPlace is where a body's declaration starts: the file a reader sees, past any //line directive, and its offset
// in the file the check parsed.
type BodyPlace struct {
	File   string
	Offset int
}

// RenderedFact is one fact of a body: an expression's type, or with Use an identifier's use, between Start and End,
// its offsets in the file, rendered as its renderings' text at Text. Unknown marks a use of an object with no stable
// identity, which matches no other fact, another unknown's included.
type RenderedFact struct {
	Start   int32
	End     int32
	Use     bool
	Unknown bool
	Text    uint32
}

// Body is the body at place as it was rendered, and whether it was.
func (renderings *BodyRenderings) Body(place BodyPlace) (RenderedBody, bool) {
	rendered, isRendered := renderings.bodies[place]
	return rendered, isRendered
}

// Text is fact's rendering.
func (renderings *BodyRenderings) Text(fact RenderedFact) string {
	return renderings.texts[fact.Text]
}

// RenderBodies renders the bodies of files, own's syntax, by info, own's type information, each placed by fileSet,
// those wanted alone, every body when wanted is nil, and with namedTypes, when not nil, the named types each
// expression's type names written beside it. It is nil when info holds no types or uses to render.
func RenderBodies(
	fileSet *token.FileSet,
	own *types.Package,
	info *types.Info,
	files []*ast.File,
	wanted func(place BodyPlace) bool,
	namedTypes func(types.Type) string,
) *BodyRenderings {
	if info == nil || info.Types == nil || info.Uses == nil {
		return nil
	}
	renderer := bodyRenderer{fileSet: fileSet, own: own, info: info, identities: NewIdentityRenderer(fileSet, nil),
		namedTypes: namedTypes, qualifier: func(other *types.Package) string { return other.Path() },
		textIndex: map[string]uint32{}, renderings: &BodyRenderings{bodies: map[BodyPlace]RenderedBody{}}}
	for _, file := range files {
		tokenFile := fileSet.File(file.FileStart)
		if tokenFile == nil {
			continue
		}
		for _, declaration := range file.Decls {
			switch declaration := declaration.(type) {
			case *ast.FuncDecl:
				if declaration.Body != nil {
					roots := []ast.Node{declaration.Type, declaration.Body}
					if declaration.Recv != nil {
						roots = append(roots, declaration.Recv)
					}
					renderer.body(tokenFile, declaration.Pos(), declaration.End(), wanted, roots...)
				}
			case *ast.GenDecl:
				repeatsValues := false
				for _, spec := range declaration.Specs {
					valueSpec, isValue := spec.(*ast.ValueSpec)
					if !isValue {
						continue
					}
					// A const spec with neither type nor values repeats the last one's, whose syntax, and so every
					// fact of it, is that one's: its own body renders empty, as stage 1's record gives it that one's.
					if declaration.Tok == token.CONST && valueSpec.Type == nil && len(valueSpec.Values) == 0 {
						if repeatsValues {
							renderer.body(tokenFile, valueSpec.Pos(), valueSpec.End(), wanted)
						}
						continue
					}
					repeatsValues = len(valueSpec.Values) > 0
					if !repeatsValues {
						continue
					}
					roots := make([]ast.Node, 0, len(valueSpec.Values)+1)
					if valueSpec.Type != nil {
						roots = append(roots, valueSpec.Type)
					}
					for _, value := range valueSpec.Values {
						roots = append(roots, value)
					}
					renderer.body(tokenFile, valueSpec.Pos(), valueSpec.End(), wanted, roots...)
				}
			}
		}
	}
	return renderer.renderings
}

// compareBool orders false before true.
func compareBool(first bool, second bool) int {
	switch {
	case first == second:
		return 0
	case first:
		return 1
	}
	return -1
}
