package core

// oneDeclarationGroup is upstream's per-declaration-kind pair of settings.
//
// Both fields are pointers, and that is load bearing rather than defensive. Upstream builds
// `options.var = { uninitialized: mode.var, initialized: mode.var }`, so an unconfigured kind gets
// `undefined` in both slots, and `if (!options[key]) return;` then skips that kind entirely. A
// value type cannot tell "not configured" from a mode, so `{ "var": "never" }` would silently
// govern `let` and `const` as well. Nil means absent, exactly as `undefined` does upstream.
type oneDeclarationGroup struct {
	Uninitialized *OneDeclarationMode
	Initialized   *OneDeclarationMode
}

// configured answers upstream's `if (!options[key])`.
//
// Upstream tests the group object for truthiness, and for the object form it always assigns one, so
// the group is falsy only when the key is not one of the five it knows. Here a group with neither
// half set is the same condition and is what the decoder produces for a kind the config never
// named.
func (group oneDeclarationGroup) configured() bool {
	return group.Uninitialized != nil || group.Initialized != nil
}
