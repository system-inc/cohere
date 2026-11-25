package go_root

import (
	"errors"
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
)

// clangPlatforms are the platforms whose default C compiler is clang, cmd/dist's clangos; every other's is gcc. It is
// read only for a root that holds no zdefaultcc.go (defaultCompiler).
var clangPlatforms = []string{"darwin", "ios", "freebsd", "openbsd"}

// CCompiler is the C compiler go env's `CC` names, in this process, or fails test naming why it could not be read:
// `CC` when it is set, or else the default the toolchain was built with, as cmd/go reads it (defaultCompiler). The go
// env file is not read, since a test reads none of the machine's configuration: a machine that set `CC` there would
// differ.
func CCompiler(test testing.TB) string {
	test.Helper()
	compiler, compilerError := cCompiler()
	if compilerError != nil {
		test.Fatalf("reading go's C compiler: %v", compilerError)
	}
	return compiler
}

// cCompiler is CCompiler's answer, or why it has none.
func cCompiler() (string, error) {
	if named := os.Getenv("CC"); named != "" {
		return named, nil
	}
	root, findError := Find()
	if findError != nil {
		return "", findError
	}
	return defaultCompiler(root, build.Default.GOOS, build.Default.GOARCH)
}

// defaultCompilerPath is where, under a Go root, cmd/dist writes the default C compiler the toolchain was built with:
// cfg.DefaultCC, which cmd/go calls for go env's `CC` when `CC` is unset.
var defaultCompilerPath = filepath.Join("src", "cmd", "go", "internal", "cfg", "zdefaultcc.go")

// defaultCompiler is the C compiler the toolchain at root runs for goos and goarch when `CC` is unset, read from its
// DefaultCC as cmd/dist writes it: a switch on goos+"/"+goarch whose cases are the targets the build named, then either
// the `CC` the toolchain was built with (Homebrew's is cc) or a switch on goos returning clang for cmd/dist's clangos,
// and gcc. Only that shape is read: any other statement or case is refused, never guessed past. A root that holds no
// zdefaultcc.go, which a toolchain cmd/dist built always does, gets cmd/dist's own rule, clangPlatforms.
func defaultCompiler(root string, goos string, goarch string) (string, error) {
	path := filepath.Join(root, defaultCompilerPath)
	source, readError := os.ReadFile(path)
	if errors.Is(readError, fs.ErrNotExist) {
		if slices.Contains(clangPlatforms, goos) {
			return "clang", nil
		}
		return "gcc", nil
	}
	if readError != nil {
		return "", readError
	}
	file, parseError := parser.ParseFile(token.NewFileSet(), path, source, 0)
	if parseError != nil {
		return "", parseError
	}
	for _, declaration := range file.Decls {
		function, isFunction := declaration.(*ast.FuncDecl)
		if !isFunction || function.Name.Name != "DefaultCC" || function.Body == nil {
			continue
		}
		compiler, decideError := defaultFromStatements(function.Body.List, goos, goarch)
		if decideError != nil {
			return "", fmt.Errorf("%s's DefaultCC: %w", path, decideError)
		}
		return compiler, nil
	}
	return "", fmt.Errorf("%s declares no DefaultCC", path)
}

// defaultFromStatements runs DefaultCC's statements for goos and goarch: each switch returns its matching case's
// compiler, if a case matches, and a return ends with its own.
func defaultFromStatements(statements []ast.Stmt, goos string, goarch string) (string, error) {
	for _, statement := range statements {
		switch typed := statement.(type) {
		case *ast.SwitchStmt:
			key, keyError := switchKey(typed.Tag, goos, goarch)
			if keyError != nil {
				return "", keyError
			}
			for _, clause := range typed.Body.List {
				caseClause, isCase := clause.(*ast.CaseClause)
				if !isCase {
					return "", fmt.Errorf("a switch holds a %T, not a case", clause)
				}
				matches, matchError := caseMatches(caseClause.List, key)
				if matchError != nil {
					return "", matchError
				}
				if matches {
					return returnedString(caseClause.Body)
				}
			}
		case *ast.ReturnStmt:
			return returnedString([]ast.Stmt{typed})
		default:
			return "", fmt.Errorf("a %T, which cmd/dist writes none of", statement)
		}
	}
	return "", errors.New("no return")
}

// switchKey is what a switch's tag reads for goos and goarch: goos+"/"+goarch for a tag adding goos, a string and
// goarch, goos for a tag that is goos alone.
func switchKey(tag ast.Expr, goos string, goarch string) (string, error) {
	if identifier, isIdentifier := tag.(*ast.Ident); isIdentifier && identifier.Name == "goos" {
		return goos, nil
	}
	if outer, isBinary := tag.(*ast.BinaryExpr); isBinary && outer.Op == token.ADD {
		if inner, isInner := outer.X.(*ast.BinaryExpr); isInner && inner.Op == token.ADD {
			left, isLeft := inner.X.(*ast.Ident)
			separator, isSeparator := stringOf(inner.Y)
			right, isRight := outer.Y.(*ast.Ident)
			if isLeft && left.Name == "goos" && isSeparator && isRight && right.Name == "goarch" {
				return goos + separator + goarch, nil
			}
		}
	}
	return "", fmt.Errorf("a switch on %T, not on goos or goos+\"/\"+goarch", tag)
}

// caseMatches says whether one of a case's strings is key; a case of anything but strings is refused.
func caseMatches(expressions []ast.Expr, key string) (bool, error) {
	for _, expression := range expressions {
		value, isString := stringOf(expression)
		if !isString {
			return false, fmt.Errorf("a case of %T, not a string", expression)
		}
		if value == key {
			return true, nil
		}
	}
	return false, nil
}

// returnedString is the string a body of one return statement returns.
func returnedString(body []ast.Stmt) (string, error) {
	if len(body) != 1 {
		return "", fmt.Errorf("a case of %d statements, not one return", len(body))
	}
	returned, isReturn := body[0].(*ast.ReturnStmt)
	if !isReturn || len(returned.Results) != 1 {
		return "", errors.New("a case that is not one return of one value")
	}
	value, isString := stringOf(returned.Results[0])
	if !isString {
		return "", fmt.Errorf("a return of %T, not a string", returned.Results[0])
	}
	return value, nil
}

// stringOf is the value of a string literal, quoted or raw.
func stringOf(expression ast.Expr) (string, bool) {
	literal, isLiteral := expression.(*ast.BasicLit)
	if !isLiteral || literal.Kind != token.STRING {
		return "", false
	}
	value, unquoteError := strconv.Unquote(literal.Value)
	return value, unquoteError == nil
}

// CgoEnabled is go env's `CGO_ENABLED`, in this process, decided as cmd/go decides it, or fails test naming why it
// could not be read: `CGO_ENABLED` when it is 0 or 1, or else go/build.Default's choice for the platform, turned off
// where `CC` is unset and the toolchain's default C compiler (defaultCompiler) is on no directory of the path, as go
// does since Go 1.20 so a machine with no C compiler builds. The go env file is not read, as CCompiler says.
func CgoEnabled(test testing.TB) bool {
	test.Helper()
	switch os.Getenv("CGO_ENABLED") {
	case "1":
		return true
	case "0":
		return false
	}
	// go/build.Default decides off for a platform without cgo and for a build that targets another platform than the
	// binary's own, where go looks for no compiler.
	if !build.Default.CgoEnabled {
		return false
	}
	if os.Getenv("CC") != "" {
		return true
	}
	_, lookPathError := exec.LookPath(CCompiler(test))
	return lookPathError == nil
}
