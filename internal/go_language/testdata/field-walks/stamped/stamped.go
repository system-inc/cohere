// Package stamped asks how long until a Stamped's time.
package stamped

import (
	"time"

	"example.com/fieldwalks/platform"
)

// Until is how long until stamped.
func Until(stamped *platform.Stamped) time.Duration { return stamped.Sub(time.Now()) }
