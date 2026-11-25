package typescript

import "github.com/microsoft/TypeScript/tsc/shim/checker"

// unsafeArgumentSignature tracks which parameter the next argument lands on.
//
// This is upstream's `FunctionSignature` class, which lives in its shared util directory and is used
// by exactly one rule. It is a private type here rather than a shelf helper for that reason; if a
// second rule wants it, that is the moment to promote it.
//
// The three rest shapes are upstream's and they are not interchangeable. A rest parameter that is a
// tuple hands out its members positionally and then repeats its last, which is what makes
// `(...params: [number, string, any])` line up. A rest parameter that is an array hands out its
// element type forever. Anything else hands out the parameter type itself.
type unsafeArgumentSignature struct {
	parameterTypes []*checker.Type

	// restKind is one of the three below, or restKindNone when the signature has no rest parameter.
	restKind  unsafeArgumentRestKind
	restIndex int

	// restType carries the answer for the array and other kinds; restTypeArguments carries the
	// tuple's members. Only one is populated.
	restType          *checker.Type
	restTypeArguments []*checker.Type

	parameterTypeIndex   int
	hasConsumedArguments bool
}

// consumeRemainingArguments marks every later argument as absorbed by the rest parameter.
func (signature *unsafeArgumentSignature) consumeRemainingArguments() {
	signature.hasConsumedArguments = true
}

// nextParameterType hands out the parameter the next argument lands on, or nil when the call has
// more arguments than the signature has places for them.
//
// Nil is not an error and upstream skips rather than reports on it: too many arguments is already a
// compiler error, and reporting a type mismatch against a parameter that does not exist would name
// a type nobody wrote.
func (signature *unsafeArgumentSignature) nextParameterType() *checker.Type {
	index := signature.parameterTypeIndex
	signature.parameterTypeIndex++

	if index < len(signature.parameterTypes) && !signature.hasConsumedArguments {
		return signature.parameterTypes[index]
	}

	switch signature.restKind {
	case restKindTuple:
		if len(signature.restTypeArguments) == 0 {
			return nil
		}
		last := signature.restTypeArguments[len(signature.restTypeArguments)-1]
		if signature.hasConsumedArguments {
			return last
		}
		typeIndex := index - signature.restIndex
		if typeIndex < 0 || typeIndex >= len(signature.restTypeArguments) {
			return last
		}
		return signature.restTypeArguments[typeIndex]

	case restKindArray, restKindOther:
		return signature.restType
	}
	return nil
}
