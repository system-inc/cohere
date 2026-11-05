// Source: tsgolint `internal/utilities/builtin_symbol_likes.go`.
//
// The is-this-a-builtin questions. A rule that special-cases `Promise` or `Error` has to mean the one
// from the default library rather than a local class wearing the name, and answering that means
// walking unions, intersections, type-parameter constraints and base types before asking where the
// symbol was declared.
package type_checking

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/checker"
	"github.com/microsoft/TypeScript/tsc/shim/tspath"
	"github.com/system-inc/cohere/internal/lint/rule"
)

func ComparePaths(left string, right string, program rule.Program) int {
	caseSensitivity := tspath.CaseInsensitive
	if program.UseCaseSensitiveFileNames() {
		caseSensitivity = tspath.CaseSensitive
	}
	return tspath.ComparePaths(left, right, caseSensitivity)
}

/**
 * @example
 * ```ts
 * class DerivedClass extends Promise<number> {}
 * DerivedClass.reject
 * // ^ PromiseLike
 * ```
 */
func IsPromiseLike(
	program rule.Program,
	typeChecker *checker.Checker,
	subjectType *checker.Type) bool {
	return IsBuiltinSymbolLike(program, typeChecker, subjectType, "Promise")
}

/**
 * @example
 * ```ts
 * const value = Promise
 * value.reject
 * // ^ PromiseConstructorLike
 * ```
 */
func IsPromiseConstructorLike(
	program rule.Program,
	typeChecker *checker.Checker,
	subjectType *checker.Type,
) bool {
	return IsBuiltinSymbolLike(program, typeChecker, subjectType, "PromiseConstructor")
}

/**
 * @example
 * ```ts
 * class Foo extends Error {}
 * new Foo()
 * //   ^ ErrorLike
 * ```
 */
func IsErrorLike(
	program rule.Program,
	typeChecker *checker.Checker,
	subjectType *checker.Type) bool {
	return IsBuiltinSymbolLike(program, typeChecker, subjectType, "Error")
}

/**
 * @example
 * ```ts
 * type T = Readonly<Error>
 * //   ^ ReadonlyErrorLike
 * ```
 */
func IsReadonlyErrorLike(
	program rule.Program,
	typeChecker *checker.Checker,
	subjectType *checker.Type,
) bool {
	return IsReadonlyTypeLike(program, typeChecker, subjectType, func(subtype *checker.Type) bool {
		checker.Type_alias(subtype).TypeArguments()
		typeArgument := checker.Type_alias(subtype).TypeArguments()[0]

		return IsErrorLike(program, typeChecker, typeArgument) ||
			IsReadonlyErrorLike(program, typeChecker, typeArgument)
	})
}

/**
 * @example
 * ```ts
 * type T = Readonly<{ foo: 'bar' }>
 * //   ^ ReadonlyTypeLike
 * ```
 */
func IsReadonlyTypeLike(
	program rule.Program,
	typeChecker *checker.Checker,
	subjectType *checker.Type,
	predicate func(subType *checker.Type) bool,
) bool {
	return IsBuiltinTypeAliasLike(program, typeChecker, subjectType, func(subtype *checker.Type) bool {
		return checker.Type_alias(subtype).Symbol().Name == "Readonly" && predicate(subtype)
	})
}

type builtinPredicateMatches uint8

const (
	builtinPredicateMatchesUnknown builtinPredicateMatches = iota
	builtinPredicateMatchesFalse
	builtinPredicateMatchesTrue
)

func IsBuiltinTypeAliasLike(
	program rule.Program,
	typeChecker *checker.Checker,
	subjectType *checker.Type,
	predicate func(subType *checker.Type) bool,
) bool {
	return IsBuiltinSymbolLikeRecurser(program, typeChecker, subjectType, func(subtype *checker.Type) builtinPredicateMatches {
		aliasSymbol := checker.Type_alias(subtype)
		if aliasSymbol == nil || len(aliasSymbol.TypeArguments()) == 0 {
			return builtinPredicateMatchesFalse
		}

		if rule.IsSymbolFromDefaultLibrary(program, aliasSymbol.Symbol()) && predicate(subtype) {
			return builtinPredicateMatchesTrue
		}

		return builtinPredicateMatchesUnknown
	})
}

func IsBuiltinSymbolLike(
	program rule.Program,
	typeChecker *checker.Checker,
	subjectType *checker.Type,
	symbolNames ...string,
) bool {
	return IsBuiltinSymbolLikeRecurser(program, typeChecker, subjectType, func(subType *checker.Type) builtinPredicateMatches {
		symbol := checker.Type_symbol(subType)
		if symbol == nil {
			return builtinPredicateMatchesFalse
		}

		actualSymbolName := symbol.Name

		if Some(symbolNames, func(name string) bool { return actualSymbolName == name }) &&
			rule.IsSymbolFromDefaultLibrary(program, symbol) {
			return builtinPredicateMatchesTrue
		}

		return builtinPredicateMatchesUnknown
	})
}

func IsAnyBuiltinSymbolLike(
	program rule.Program,
	typeChecker *checker.Checker,
	subjectType *checker.Type,
) bool {
	return IsBuiltinSymbolLikeRecurser(program, typeChecker, subjectType, func(subType *checker.Type) builtinPredicateMatches {
		symbol := checker.Type_symbol(subType)
		if symbol == nil {
			return builtinPredicateMatchesFalse
		}

		if rule.IsSymbolFromDefaultLibrary(program, symbol) {
			return builtinPredicateMatchesTrue
		}

		return builtinPredicateMatchesUnknown
	})
}

func IsBuiltinSymbolLikeRecurser(
	program rule.Program,
	typeChecker *checker.Checker,
	subjectType *checker.Type,
	predicate func(subType *checker.Type) builtinPredicateMatches,
) bool {
	if IsIntersectionType(subjectType) {
		return Some(IntersectionTypeParts(subjectType), func(part *checker.Type) bool {
			return IsBuiltinSymbolLikeRecurser(program, typeChecker, part, predicate)
		})
	}
	if IsUnionType(subjectType) {
		return Every(UnionTypeParts(subjectType), func(part *checker.Type) bool {
			return IsBuiltinSymbolLikeRecurser(program, typeChecker, part, predicate)
		})
	}
	if IsTypeParameter(subjectType) {
		constraint := checker.Checker_getBaseConstraintOfType(typeChecker, subjectType)

		if constraint != nil {
			return IsBuiltinSymbolLikeRecurser(program, typeChecker, constraint, predicate)
		}

		return false
	}

	predicateResult := predicate(subjectType)
	if predicateResult == builtinPredicateMatchesTrue {
		return true
	} else if predicateResult == builtinPredicateMatchesFalse {
		return false
	}

	symbol := checker.Type_symbol(subjectType)
	if symbol != nil && symbol.Flags&(ast.SymbolFlagsClass|ast.SymbolFlagsInterface) != 0 {
		declaredType := checker.Checker_getDeclaredTypeOfSymbol(typeChecker, symbol)
		for _, baseType := range checker.Checker_getBaseTypes(typeChecker, declaredType) {
			if IsBuiltinSymbolLikeRecurser(program, typeChecker, baseType, predicate) {
				return true
			}
		}
	}
	return false
}
