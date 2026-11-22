// Package stripped holds a function whose body the strip takes away, the only user of an import, beside one it keeps.
package stripped

import "strings"

func kept(text string) int { return len(text) }

func cut(text string) string { return strings.ToUpper(text) }
