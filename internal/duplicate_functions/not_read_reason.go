package duplicate_functions

// NotReadReason is why a function was never read at all, as opposed to read and refused.
type NotReadReason string

const (
	// NotReadBodyless is a declaration without a body: an overload, an ambient or abstract one, a Go function
	// written in assembly.
	NotReadBodyless NotReadReason = "bodyless"

	// NotReadGenerated is a function in a generated file, which a tool owns.
	NotReadGenerated NotReadReason = "generated"

	// NotReadCopied is a function in a file marked `// cohere:copied`, a copy of upstream source kept byte for byte,
	// which every house rule already withholds.
	NotReadCopied NotReadReason = "copied"
)
