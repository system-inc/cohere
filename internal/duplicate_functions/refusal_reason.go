package duplicate_functions

// RefusalReason is why a function was refused: a closed set, so a reader can count them, and a new gap is a new
// reason rather than a string nobody grouped.
type RefusalReason string

const (
	// RefusalParameterDefault is a parameter default that is not a literal. Lowering drops every default, and only a
	// literal's value can be put back into the hash.
	RefusalParameterDefault RefusalReason = "parameterDefault"

	// RefusalObjectMethodCapture is an object method that closes over something. Lowering records no captures for
	// an object method, so what it reads from outside is unknown.
	RefusalObjectMethodCapture RefusalReason = "objectMethodCapture"

	// RefusalFieldInitializers is a constructor of a class with instance field initializers, which run as part of
	// construction and are not in the constructor's IR.
	RefusalFieldInitializers RefusalReason = "fieldInitializers"

	// RefusalUnresolvedCapture is a closure capture lowering could not resolve.
	RefusalUnresolvedCapture RefusalReason = "unresolvedCapture"

	// RefusalDynamicImport is `import()`, which lowering does not model.
	RefusalDynamicImport RefusalReason = "dynamicImport"

	// RefusalUnsupportedNode is any other syntax lowering does not model, named in the detail.
	RefusalUnsupportedNode RefusalReason = "unsupportedNode"

	// RefusalUnsupportedInstruction is a Go instruction the hash does not model, named in the detail.
	RefusalUnsupportedInstruction RefusalReason = "unsupportedInstruction"

	// RefusalNotHashable is an IR the hash cannot walk as it expects, a value used before it is defined say, named in
	// the detail. Refused rather than hashed, since a hash over a misread graph could join two different functions.
	RefusalNotHashable RefusalReason = "notHashable"
)
