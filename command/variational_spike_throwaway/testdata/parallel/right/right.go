// Package right counts shapes with maps and spells them with strconv.
package right

import (
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"

	"example.com/parallel/shape"
)

// Tally counts shapes by name, safe for many goroutines.
type Tally struct {
	mutex  sync.Mutex
	counts map[string]int
}

// Add counts one shape.
func (tally *Tally) Add(each shape.Shape) {
	tally.mutex.Lock()
	defer tally.mutex.Unlock()
	if tally.counts == nil {
		tally.counts = map[string]int{}
	}
	tally.counts[each.Name]++
}

// String spells the tally, names in order.
func (tally *Tally) String() string {
	names := slices.Sorted(maps.Keys(tally.counts))
	spelled := make([]string, 0, len(names))
	for _, name := range names {
		spelled = append(spelled, name+"="+strconv.Itoa(tally.counts[name]))
	}
	return strings.Join(spelled, ",")
}
