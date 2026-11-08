// Package shadowed declares its own delete at package scope, which is not the universe's, so neither reports here.
package shadowed

func delete(values map[string]int, key string) {}

func guarded(values map[string]int, key string) {
	if _, ok := values[key]; ok {
		delete(values, key)
	}
}
