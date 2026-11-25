//go:build !windows

package gate

// The same import as windows's file, so the two builds' imports are equal and the body reach leaves the package's
// shared bodies to the facts' reach, which a differing import would reach whole (#bed0mnk).
import _ "testing"

var unrelated int
