package type_predicates

import "go/types"

// RuntimeCgoPath is the import path whose presence among a package's imports says it uses cgo: the cgo tool's
// translation adds the import, and so does the load for a package whose cgo files it type-checked with package "C"
// faked.
const RuntimeCgoPath = "runtime/cgo"

// UsesCgo reports whether checked's types import runtime/cgo directly. Two places must answer this alike, and both
// call it (#b2t9xmf): vet's cgocall judges a package only when it does, and the load marks every such package never
// restored from the host's run. The restore leaves cgocall's property out of a foreign field's rendering because of
// that pairing, so if either stopped calling UsesCgo the dropped property would start to matter with nothing to say
// so.
func UsesCgo(checked *types.Package) bool {
	for _, imported := range checked.Imports() {
		if imported.Path() == RuntimeCgoPath {
			return true
		}
	}
	return false
}
