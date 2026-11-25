package printf

// wrapperKind is how a function behaves among fmt's: x/tools' printf Kind.
type wrapperKind int

const (
	kindNone   wrapperKind = iota // Not a wrapper of fmt's functions.
	kindPrint                     // Behaves like fmt.Print.
	kindPrintf                    // Behaves like fmt.Printf.
	kindErrorf                    // Behaves like fmt.Errorf.
)

// String is the kind as the message of a forwarding call without `...` names it.
func (kind wrapperKind) String() string {
	switch kind {
	case kindPrint:
		return "print"
	case kindPrintf:
		return "printf"
	case kindErrorf:
		return "errorf"
	}
	return "(none)"
}
