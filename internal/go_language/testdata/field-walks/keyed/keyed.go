// Package keyed keys a context's value by a Key.
package keyed

import (
	"context"

	"example.com/fieldwalks/platform"
)

// With holds one under key.
func With(key *platform.Key) context.Context { return context.WithValue(context.Background(), *key, 1) }
