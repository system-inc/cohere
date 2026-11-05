package configuration

// ruleReach is which registered rules each config key configures, by the resolver's own test, each
// key asked once.
//
// Matching one key to the inherited spellings of its ruling compares it with every key the layers
// below wrote, and each comparison asked the whole registry about both keys again: on ahra that was a
// fresh 464-name scan, and a fresh map, for every pair, and it was 590ms of every run, the whole of
// what the run spent outside its phases (#kgv1pry). A key reaches the same rules every time it is
// asked, so each is asked once per load. One load runs on one goroutine, so a plain map does.
type ruleReach struct {
	registeredNames []string
	byKey           map[string]map[string]bool
}

// of is the registered rules key configures.
func (reach *ruleReach) of(key string) map[string]bool {
	if reached, found := reach.byKey[key]; found {
		return reached
	}
	reached := map[string]bool{}
	for _, registered := range reach.registeredNames {
		if KeyReachesRule(key, registered) {
			reached[registered] = true
		}
	}
	reach.byKey[key] = reached
	return reached
}

// sameRuling reports whether two differently spelled keys configure a registered rule in common.
//
// Decided by the registry rather than by the names. `nexus/x` and `x` look like one ruling and are,
// when only `x` is registered. `@typescript-eslint/no-invalid-this` and `no-invalid-this` look the same
// way and are two rules, a core rule and its typescript-eslint twin; they still share a ruling, because
// the resolver lets the qualified key configure the core rule when no key names it exactly (#hprjh4s).
func (reach *ruleReach) sameRuling(left string, right string) bool {
	leftReached := reach.of(left)
	for registered := range reach.of(right) {
		if leftReached[registered] {
			return true
		}
	}
	return false
}

// reachesEvery reports whether key reaches every registered rule inherited reaches, so that inherited
// can be removed without leaving any rule it configured unconfigured.
func (reach *ruleReach) reachesEvery(key string, inherited string) bool {
	reached := reach.of(key)
	for registered := range reach.of(inherited) {
		if !reached[registered] {
			return false
		}
	}
	return true
}
