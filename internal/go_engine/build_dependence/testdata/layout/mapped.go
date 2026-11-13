package layout

// Mapped is declared under a line directive, as a cgo package's translated declarations are, so a reader sees it in
// mapped.source, and the graph names its unit there.
//
//line mapped.source:1
func Mapped() int { return 1 }
