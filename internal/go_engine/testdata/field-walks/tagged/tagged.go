// Package tagged embeds a Tagged and a Titled, whose json names meet one level down.
package tagged

import "example.com/fieldwalks/platform"

// Mine embeds both.
type Mine struct {
	platform.Tagged
	platform.Titled
}
