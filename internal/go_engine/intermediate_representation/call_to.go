package intermediate_representation

import "slices"

// IsCallTo reports whether common calls the function CallName spells as name. It is staticcheck's irutil.IsCallTo
// (honnef.co/go/tools@v0.8.1 go/ir/irutil/util.go), translated onto our IR.
func IsCallTo(common *CallCommon, name string) bool { return CallName(common) == name }

// IsCallToAny reports whether common calls any of the functions CallName spells as names. It is staticcheck's
// irutil.IsCallToAny (honnef.co/go/tools@v0.8.1 go/ir/irutil/util.go), translated onto our IR.
func IsCallToAny(common *CallCommon, names ...string) bool {
	return slices.Contains(names, CallName(common))
}
