package duplicate_functions

// Function is one function the hash read whole.
type Function struct {
	// File is the function's file, absolute. A per-file cache entry leaves it empty, since the entry names it.
	File string `json:"file"`

	// Line is where the function starts, 1-based: its declaration, or the expression of a function value.
	Line int `json:"line"`

	// Name is the function as a reader finds it: `Class.method`, a function's own name, or the variable a function
	// value is assigned to.
	Name string `json:"name"`

	// Size is the instructions that compute something, which the floor is measured in. Each engine counts its own
	// IR's (see each hasher), and the floor was measured on each.
	Size int `json:"size"`

	Digest Digest `json:"digest"`

	// Kept is the reason a suppression gives for keeping this function in its shape, empty when none covers it.
	Kept string `json:"kept,omitempty"`
}

// Refusal is one function the hash could not read faithfully, refused by name rather than guessed past, so the report
// says what it did not read.
type Refusal struct {
	File   string        `json:"file"`
	Line   int           `json:"line"`
	Name   string        `json:"name"`
	Reason RefusalReason `json:"reason"`

	// Detail says more where the reason alone does not: the syntax kind of an unsupported node, the instruction an
	// engine does not model.
	Detail string `json:"detail,omitempty"`
}

// Crash is a file, or a package by its first file, the hash could not read at all, and why. Its functions are in no
// count, so the report names it rather than reading as complete.
type Crash struct {
	File  string `json:"file"`
	Error string `json:"error"`
}

// Place is a line of a file: where a suppression sits.
type Place struct {
	File string `json:"file"`
	Line int    `json:"line"`
}

// Group is two or more functions with one digest: one idea written more than once.
type Group struct {
	Digest Digest `json:"digest"`

	// Size is the smallest member's, which for members of one digest differs only where an engine counts a part it
	// hashes by identity.
	Size int `json:"size"`

	// Members are ordered by file and line.
	Members []Function `json:"members"`
}

// CacheUse is what the report read from its cache: per file in TypeScript, per package in Go.
type CacheUse struct {
	Unit   string `json:"unit"`
	Read   int    `json:"read"`
	Hashed int    `json:"hashed"`
}
