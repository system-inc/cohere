// Package subpackage is not main-like: it imports cobra's doc subpackage, which imports cobra, but not cobra itself.
package subpackage

import "github.com/spf13/cobra/doc"

// Usage is a command's usage.
var Usage = doc.Describe
