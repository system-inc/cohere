package corpus

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// PinnedFile is the file, from this repository's root, whose constants name every commit the land gate lays a corpus
// at. The gate reads it from the commit it lands rather than from its own build, so a landing that moves a pin is
// gated against the corpus at the pin it moves to (#5r0vrte).
const PinnedFile = "internal/corpus/corpus_pinned.go"

// Pins is what the land gate lays of one corpus on a gate box (#5r0vrte).
type Pins struct {
	// Checkout is the commit the laid tree is, which a test reading Root reads on a gate box: the constant named
	// the corpus's Identifier followed by Pin, such as AhraPin.
	Checkout string
	// Held is every commit a test reads through At, which the laid repository holds: Checkout and each other constant
	// whose name begins with the corpus's Identifier and ends in Pin, such as StructureCountPin, sorted.
	Held []string
}

// commitPattern is a whole commit name, which is all a pin may be.
var commitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

// PinsIn reads every corpus's Pins from source, a copy of PinnedFile. A corpus with no checkout constant, or a pin
// constant that isn't a whole commit, is an error, so a landing can't lay a corpus at a name it misread.
func PinsIn(source []byte) (map[string]Pins, error) {
	parsed, parseError := parser.ParseFile(token.NewFileSet(), PinnedFile, source, parser.SkipObjectResolution)
	if parseError != nil {
		return nil, parseError
	}
	values := map[string]string{}
	for _, declaration := range parsed.Decls {
		general, isGeneral := declaration.(*ast.GenDecl)
		if !isGeneral || general.Tok != token.CONST {
			continue
		}
		for _, specification := range general.Specs {
			value, isValue := specification.(*ast.ValueSpec)
			if !isValue {
				continue
			}
			for index, name := range value.Names {
				if !strings.HasSuffix(name.Name, "Pin") || index >= len(value.Values) {
					continue
				}
				literal, isLiteral := value.Values[index].(*ast.BasicLit)
				if !isLiteral || literal.Kind != token.STRING {
					return nil, fmt.Errorf("%s: %s is no string, so it names no commit", PinnedFile, name.Name)
				}
				// Ignored: a string literal the parser accepted always unquotes.
				commit, _ := strconv.Unquote(literal.Value)
				if !commitPattern.MatchString(commit) {
					return nil, fmt.Errorf("%s: %s is %q, which is no whole commit", PinnedFile, name.Name, commit)
				}
				values[name.Name] = commit
			}
		}
	}
	pins := map[string]Pins{}
	for _, corpus := range All {
		checkout, found := values[corpus.Identifier+"Pin"]
		if !found {
			return nil, fmt.Errorf("%s names no %sPin, the commit the land gate lays %s at", PinnedFile,
				corpus.Identifier, corpus.Name)
		}
		held := []string{}
		for name, commit := range values {
			if strings.HasPrefix(name, corpus.Identifier) && !slices.Contains(held, commit) {
				held = append(held, commit)
			}
		}
		slices.Sort(held)
		pins[corpus.Name] = Pins{Checkout: checkout, Held: held}
	}
	return pins, nil
}
