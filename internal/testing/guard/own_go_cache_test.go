package guard

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// ownGoCachePackage is the seal's package, whose Environment and Set a declaration hands its environment through.
const ownGoCachePackage = "own_go_cache"

// A declaration that hands a go command a `GOCACHE` of its own also empties `GOCACHEPROG` (#e94a6r4). With a cache
// program named, go reads and writes every entry through the program and leaves the `GOCACHE` directory empty, and
// Loom's runner names one for every integrate job, which each test it runs inherits: the first integration under it
// went red on two go_cache tests that trimmed an empty directory, and on an integrate-job test that read the runner's
// pair as its own. The seal is own_go_cache.Environment or own_go_cache.Set, or in a module that can't import cohere,
// a function of its own that names both.
//
// Read from the syntax alone: an environment entry is a string literal starting `GOCACHE=` (or that literal plus
// something) that is an element of a composite literal or an argument of append, and a `Setenv` call whose first
// argument is the literal `GOCACHE`. The seal, in the same top-level declaration, is named at cacheHandsAndSeals. A
// comparison against a `GOCACHE=` line a stand-in wrote hands nothing and isn't read.
func TestAGoCacheOfItsOwnEmptiesTheCacheProgram(test *testing.T) {
	test.Parallel()
	root, absolutePathError := filepath.Abs(filepath.Join("..", "..", ".."))
	if absolutePathError != nil {
		test.Fatal(absolutePathError)
	}
	files := token.NewFileSet()
	unsealed := []string{}
	sealed := 0
	walkError := filepath.WalkDir(root, func(path string, entry fs.DirEntry, pathError error) error {
		if pathError != nil {
			return pathError
		}
		native, relativeError := filepath.Rel(root, path)
		if relativeError != nil {
			return relativeError
		}
		relative := filepath.ToSlash(native)
		if entry.IsDir() {
			// The vendored compiler, caches and fixtures hold no go command of ours.
			if relative != "." &&
				(strings.HasPrefix(entry.Name(), ".") || entry.Name() == "testdata" || relative == "TypeScript") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		parsed, parseError := parser.ParseFile(files, path, nil, parser.SkipObjectResolution)
		if parseError != nil {
			return parseError
		}
		for _, declaration := range parsed.Decls {
			hands, seals := cacheHandsAndSeals(declaration, parsed.Name.Name == ownGoCachePackage)
			if len(hands) == 0 {
				continue
			}
			if seals {
				sealed++
				continue
			}
			for _, hand := range hands {
				unsealed = append(unsealed, files.Position(hand).String())
			}
		}
		return nil
	})
	if walkError != nil {
		test.Fatal(walkError)
	}
	// The guard read something: own_go_cache itself and go_cache's own copy each hand and seal a cache.
	if sealed < 2 {
		test.Fatalf("read %d sealed declarations under %s, want at least own_go_cache's and go_cache's", sealed, root)
	}
	for _, hand := range unsealed {
		test.Errorf(
			"%s hands a go command a GOCACHE of its own without emptying GOCACHEPROG: under a cache program, "+
				"as Loom's runner names one, go writes through the program and the directory stays empty; use "+
				"own_go_cache.Environment or own_go_cache.Set",
			hand,
		)
	}
}

// cacheHandsAndSeals reads one declaration: where it hands a go command a `GOCACHE`, and whether it seals it, an entry
// that is exactly `GOCACHEPROG=`, a `Setenv` of `GOCACHEPROG` to the empty literal, or a call of own_go_cache's
// Environment or Set, named bare inside that package (inOwnGoCache). A `GOCACHEPROG` naming a program seals nothing.
func cacheHandsAndSeals(declaration ast.Decl, inOwnGoCache bool) ([]token.Pos, bool) {
	hands := []token.Pos{}
	seals := false
	entry := func(expression ast.Expr) {
		if literal, ok := expression.(*ast.BasicLit); ok && literal.Kind == token.STRING &&
			leadingString(literal) == "GOCACHEPROG=" {
			seals = true
		}
		if strings.HasPrefix(leadingString(expression), "GOCACHE=") {
			hands = append(hands, expression.Pos())
		}
	}
	sealing := map[string]bool{"Environment": true, "Set": true}
	ast.Inspect(declaration, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.CompositeLit:
			for _, element := range typed.Elts {
				entry(element)
			}
		case *ast.CallExpr:
			if identifier, ok := typed.Fun.(*ast.Ident); ok && identifier.Name == "append" && len(typed.Args) > 1 {
				for _, argument := range typed.Args[1:] {
					entry(argument)
				}
			}
			if identifier, ok := typed.Fun.(*ast.Ident); ok && inOwnGoCache && sealing[identifier.Name] {
				seals = true
			}
			selector, ok := typed.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if qualifier, ok := selector.X.(*ast.Ident); ok && qualifier.Name == ownGoCachePackage &&
				sealing[selector.Sel.Name] {
				seals = true
			}
			if selector.Sel.Name == "Setenv" && len(typed.Args) == 2 {
				empty, ok := typed.Args[1].(*ast.BasicLit)
				switch leadingString(typed.Args[0]) {
				case "GOCACHEPROG":
					seals = seals || ok && leadingString(empty) == ""
				case "GOCACHE":
					hands = append(hands, typed.Pos())
				}
			}
		}
		return true
	})
	return hands, seals
}

// leadingString is the value of the string literal expression is or begins with, `"GOCACHE=" + cache` read as
// `GOCACHE=`, and empty for anything else.
func leadingString(expression ast.Expr) string {
	leading := expression
	for {
		binary, ok := leading.(*ast.BinaryExpr)
		if !ok || binary.Op != token.ADD {
			break
		}
		leading = binary.X
	}
	literal, ok := leading.(*ast.BasicLit)
	if !ok || literal.Kind != token.STRING {
		return ""
	}
	value, unquoteError := strconv.Unquote(literal.Value)
	if unquoteError != nil {
		return ""
	}
	return value
}
