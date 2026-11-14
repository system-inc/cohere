// Package platform holds settings whose layout every build shares, with a part holding a count no importer can name.
package platform

import (
	"fmt"

	"example.com/aliasrecords/base"
)

// Settings holds a part of an unnamed type, which holds a count of its own.
var Settings struct{ Part struct{ count base.Count } }

// Own writes out the part's count.
func Own() string { return fmt.Sprint(Settings.Part.count) }

// Zero is no count, which keeps base in use once the part counts with a Holder.
const Zero base.Count = 0

// Holder holds a Lock, whose fields windows declares its own way, under a name every build declares alike: its name
// reaches no Lock's, so only a read of its layout is windows' own.
type Holder struct{ lock Lock }

// Describe writes out what holder holds.
func Describe(holder Holder) string { return fmt.Sprint(holder.lock) }
