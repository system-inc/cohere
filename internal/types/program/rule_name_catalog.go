package program

import "strings"

// ruleNameCatalog answers whether a rule name a suppression comment wrote exists anywhere cohere can
// see: a registered rule, or a key in the config at any severity (a decision recorded about a rule
// cohere has not ported yet still proves the rule exists).
//
// "Exists" is deliberately the same test `suppression.Directive.Covers` applies: the name equals a
// known one, or qualifies it with a plugin prefix on a `/` boundary. A looser test would stay silent
// on a directive that silences nothing, which is the case this exists to report.
type ruleNameCatalog struct {
	names map[string]bool

	// registeredByBareName maps a registered rule's name without its plugin prefix to the full name,
	// so a directive written under a stale prefix (`structure/x` for `nexus/x`) can be told what it
	// probably meant.
	registeredByBareName map[string]string
}

func (catalog *ruleNameCatalog) exists(name string) bool {
	if catalog.names[name] {
		return true
	}
	for known := range catalog.names {
		if strings.HasSuffix(name, "/"+known) {
			return true
		}
	}
	return false
}
