package contract

// ProvenanceRecord says which engine wrote the stream and from what source.
//
// The contract's provenance also lists `swiftSyntax` and `swiftFormat`, the versions of two Swift
// libraries. They are left out here: a Go engine links neither, and an empty version would read as one
// it could not find rather than one it does not have. The front door ignores a field it is not sent.
type ProvenanceRecord struct {
	Contract int    `json:"contract"`
	Engine   string `json:"engine"`
	Version  string `json:"version"`
	// Commit is 40 hex digits, or `dev`.
	Commit string `json:"commit"`
	// SourceTreeModified drives the warning that no commit reproduces these findings.
	SourceTreeModified bool `json:"sourceTreeModified"`
	// Toolchain is the Go toolchain the engine was built with, as runtime.Version spells it.
	Toolchain string `json:"toolchain"`
}

func (ProvenanceRecord) kind() Kind { return KindProvenance }
