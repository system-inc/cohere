//go:build !windows

// Package q declares Old in a file of each platform's, deprecated on every platform but windows, for the facts' reach
// (#5vbs1np): a deprecation is read from its declaration's doc comment, so it differs between two builds only where
// the declaration does, as here, where each build compiles a file of its own.
package q

// Old is every platform's but windows's.
//
// Deprecated: windows alone still needs it.
func Old() {}
