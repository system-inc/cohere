// Source: tsgolint `internal/utilities/utils.go`.
//
// Node and range helpers, plus the generic slice functions the rest of this package is written in
// terms of. The slice helpers carry their own `Source:` lines: they came to tsgolint from
// typescript-go's `internal/core/core.go`, which our shim does not re-export, so this is the only
// copy reachable from our code rather than a second one.
package type_checking

import (
	"iter"
	"slices"
	"unicode"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/checker"
	"github.com/microsoft/TypeScript/tsc/shim/core"
	"github.com/microsoft/TypeScript/tsc/shim/scanner"
)

// commentRangeFactory is the one factory every GetCommentsInRange call hands the scanner.
//
// The scanner's only use of its factory is NewCommentRange, which builds a CommentRange by value and
// reads and writes nothing on the factory (scanner.go's iterateCommentRanges, ast.go's
// NewCommentRange). So one shared factory serves every call on every goroutine. A factory built per
// call was 30 MB in 22K objects on a cold ahra run, each one about 1.4 KB of node pools never used.
// TestCommentRangeFactoryStaysUntouched fails if the scanner ever starts writing to it, which is the
// day this sharing would become a race.
var commentRangeFactory ast.NodeFactory

func GetCommentsInRange(sourceFile *ast.SourceFile, inRange core.TextRange) iter.Seq[ast.CommentRange] {
	return func(yield func(ast.CommentRange) bool) {
		for commentRange := range scanner.GetTrailingCommentRanges(
			&commentRangeFactory,
			sourceFile.Text(),
			inRange.Pos(),
		) {
			if commentRange.Pos() >= inRange.End() {
				break
			}
			if !yield(commentRange) {
				return
			}
		}

		for commentRange := range scanner.GetLeadingCommentRanges(
			&commentRangeFactory,
			sourceFile.Text(),
			inRange.Pos(),
		) {
			if commentRange.Pos() >= inRange.End() {
				break
			}
			if !yield(commentRange) {
				return
			}
		}
	}
}

func HasCommentsInRange(sourceFile *ast.SourceFile, inRange core.TextRange) bool {
	for range GetCommentsInRange(sourceFile, inRange) {
		return true
	}
	return false
}

func TypeRecurser(subjectType *checker.Type, predicate func(subjectType *checker.Type) /* should stop */ bool) bool {
	if IsTypeFlagSet(subjectType, checker.TypeFlagsUnionOrIntersection) {
		for _, subtype := range subjectType.Types() {
			if TypeRecurser(subtype, predicate) {
				return true
			}
		}
		return false
	}
	return predicate(subjectType)
}

// Super dirty hack for optional fields :(
func Reference[T any](value T) *T {
	// A copy of its own, so the pointer handed out reaches nothing the caller passed.
	copied := value
	return &copied
}

func GetNumberIndexType(typeChecker *checker.Checker, subjectType *checker.Type) *checker.Type {
	return checker.Checker_getIndexTypeOfType(typeChecker, subjectType, checker.Checker_numberType(typeChecker))
}
func GetHeritageClauses(node *ast.Node) *ast.NodeList {
	switch node.Kind {
	case ast.KindClassDeclaration:
		return node.AsClassDeclaration().HeritageClauses
	case ast.KindClassExpression:
		return node.AsClassExpression().HeritageClauses
	case ast.KindInterfaceDeclaration:
		return node.AsInterfaceDeclaration().HeritageClauses
	}
	return nil
}

// Source: TypeScript/tsc/internal/core/core.go
func Filter[T any](slice []T, predicate func(T) bool) []T {
	for index, value := range slice {
		if !predicate(value) {
			result := slices.Clone(slice[:index])
			for index++; index < len(slice); index++ {
				value = slice[index]
				if predicate(value) {
					result = append(result, value)
				}
			}
			return result
		}
	}
	return slice
}

// Source: TypeScript/tsc/internal/core/core.go
func FilterIndex[T any](slice []T, predicate func(T, int, []T) bool) []T {
	for index, value := range slice {
		if !predicate(value, index, slice) {
			result := slices.Clone(slice[:index])
			for index++; index < len(slice); index++ {
				value = slice[index]
				if predicate(value, index, slice) {
					result = append(result, value)
				}
			}
			return result
		}
	}
	return slice
}

// Source: TypeScript/tsc/internal/core/core.go
func Map[T, U any](slice []T, transform func(T) U) []U {
	if len(slice) == 0 {
		return nil
	}
	result := make([]U, len(slice))
	for index, value := range slice {
		result[index] = transform(value)
	}
	return result
}

// Source: TypeScript/tsc/internal/core/core.go
func Some[T any](slice []T, predicate func(T) bool) bool {
	for _, value := range slice {
		if predicate(value) {
			return true
		}
	}
	return false
}

// Source: TypeScript/tsc/internal/core/core.go
func Every[T any](slice []T, predicate func(T) bool) bool {
	for _, value := range slice {
		if !predicate(value) {
			return false
		}
	}
	return true
}

// Source: TypeScript/tsc/internal/core/core.go
func Flatten[T any](array [][]T) []T {
	var result []T
	for _, subArray := range array {
		result = append(result, subArray...)
	}
	return result
}

func IncludesModifier(node interface{ Modifiers() *ast.ModifierList }, modifier ast.Kind) bool {
	modifiers := node.Modifiers()
	if modifiers == nil {
		return false
	}
	return Some(modifiers.NodeList.Nodes, func(node *ast.Node) bool {
		return node.Kind == modifier
	})
}

// Source:
// https://github.com/microsoft/typescript-go/blob/5652e65d5ae944375676d3955f9755e554576d41/internal/jsnum/string.go#L99
func IsStrWhiteSpace(character rune) bool {
	// This is different than stringutil.IsWhiteSpaceLike.

	// https://tc39.es/ecma262/2024/multipage/ecmascript-language-lexical-grammar.html#prod-LineTerminator
	// https://tc39.es/ecma262/2024/multipage/ecmascript-language-lexical-grammar.html#prod-WhiteSpace

	switch character {
	// LineTerminator
	case '\n', '\r', 0x2028, 0x2029:
		return true
	// WhiteSpace
	case '\t', '\v', '\f', 0xFEFF:
		return true
	}

	// WhiteSpace
	return unicode.Is(unicode.Zs, character)
}
