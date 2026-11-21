//go:build !windows

package gate

// The import spells go-flags with an escape, which names the same path windows's file imports.
import _ "github.com/jessevdk/go\x2dflags"

func Other() {}
