// Package provider hands out a holder.Box, so a package importing only provider can read its field.
package provider

import "indexed/holder"

const Value = 1

var Shared = holder.Box{Count: 1}
