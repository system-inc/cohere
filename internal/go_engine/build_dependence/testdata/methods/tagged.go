//go:build dependence_tag

package methods

// Tagged is declared in the tag build only.
func (Count) Tagged() bool { return true }
