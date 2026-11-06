// Package allowed holds imports the rule leaves alone: plain, blank and dot imports, a real rename, and an alias equal
// to the package's name that the path's last element does not spell, which tells the reader the name the path hides.
package allowed

import (
	_ "embed"
	"fmt"
	. "strings"

	widget "go-widget"
	pathpackage "path"
	versioned "versioned/v2"
	yaml "yaml.v3"
)

// Used uses every named import, so the fixture compiles.
var Used = fmt.Sprint(ToUpper("a"), pathpackage.Base("a/b"), widget.Size, versioned.Version, yaml.Indent)
