package check

import (
	"bytes"
	"crypto/sha256"
	"maps"
	"slices"
	"strings"
	"sync"

	"github.com/system-inc/cohere/internal/go_engine/cache"
)

// aliasPairSize is one alias in a table's bytes: the alias, then the key of the entry it stands for.
const aliasPairSize = 2 * len(cache.Key{})

// aliasTableFloor is the fewest aliases a table keeps, however few packages a run reaches, so a small project's
// earlier states stay findable for a while.
const aliasTableFloor = 1024

// aliasTable is one build's aliases, each key that stands for an entry the store keeps under another key, a second key
// or the first key of a package a second key restored, kept in the store as one file under the build's own key
// (#m7w1ncy). A file each, as before, was nearly half of a cold run's store at cohere's root, about 12,000 small writes
// in 26,000, and most of its kernel file calls. Each run reads the table once and writes it once for each round of
// aliases it adds, the newest first, and keeps the newest up to a bound, so the aliases of states long gone age out.
// An alias is exact whoever wrote it, since both keys are digests of what went in, so a union of tables is never
// wrong: two runs of one build writing at once can lose the other's new aliases, which the next run misses and writes
// again, never reads wrongly.
type aliasTable struct {
	store *cache.Store
	key   cache.Key

	// loading reads the table from the store once, on its first use.
	loading sync.Once

	// lock guards targets and order, which a write replaces while reads of other packages may look up.
	lock sync.Mutex

	// targets is the key each alias stands for, and order the aliases newest first.
	targets map[cache.Key]cache.Key
	order   []cache.Key
}

// newAliasTable is the table of the build context names, kept in store: one table for each context, as every key
// it holds is one, since each folds the context in.
func newAliasTable(store *cache.Store, context cache.Context) *aliasTable {
	named := strings.Join(slices.Concat([]string{"alias table", context.FormatVersion, context.EngineIdentity},
		context.Build, []string{"rules"}, context.RuleSet), "\x00")
	return &aliasTable{store: store, key: sha256.Sum256([]byte(named))}
}

// load reads the table from the store, once. A table the store does not hold whole, or whose length is no whole
// number of aliases, is an empty one: every key it held misses, and the next write starts it again.
func (table *aliasTable) load() {
	table.loading.Do(func() {
		table.targets = map[cache.Key]cache.Key{}
		contents, stored := table.store.Get(table.key)
		if !stored || len(contents)%aliasPairSize != 0 {
			return
		}
		for start := 0; start < len(contents); start += aliasPairSize {
			alias := cache.Key(contents[start : start+len(cache.Key{})])
			if _, seen := table.targets[alias]; seen {
				continue
			}
			table.targets[alias] = cache.Key(contents[start+len(cache.Key{}) : start+aliasPairSize])
			table.order = append(table.order, alias)
		}
	})
}

// target is the key the entry alias stands for is stored under, and whether the table holds alias.
func (table *aliasTable) target(alias cache.Key) (cache.Key, bool) {
	table.load()
	table.lock.Lock()
	defer table.lock.Unlock()
	target, held := table.targets[alias]
	return target, held
}

// add keeps aliases in the table, before every alias it held, and writes the table to the store once, the newest
// limit of them. An alias already held is moved to the front with its new target. Nothing new is no write.
func (table *aliasTable) add(aliases map[cache.Key]cache.Key, limit int) error {
	if len(aliases) == 0 {
		return nil
	}
	table.load()
	table.lock.Lock()
	defer table.lock.Unlock()
	// Sorted, so one round of aliases writes the same bytes whatever order the map gives them in.
	added := slices.SortedFunc(maps.Keys(aliases), func(left cache.Key, right cache.Key) int {
		return bytes.Compare(left[:], right[:])
	})
	order := slices.Concat(added, slices.DeleteFunc(table.order, func(alias cache.Key) bool {
		_, readded := aliases[alias]
		return readded
	}))
	for alias, target := range aliases {
		table.targets[alias] = target
	}
	if len(order) > limit {
		for _, dropped := range order[limit:] {
			delete(table.targets, dropped)
		}
		order = order[:limit]
	}
	table.order = order
	contents := make([]byte, 0, len(order)*aliasPairSize)
	for _, alias := range order {
		target := table.targets[alias]
		contents = append(append(contents, alias[:]...), target[:]...)
	}
	return table.store.Put(table.key, contents)
}
