// Package facts_importer imports facts, so the planted fact analyzers run on facts as its dependency, and declares a
// First of its own, so a pair that marks First exports a fact on the fixture package itself as well.
package facts_importer

import "facts"

func First() { facts.First() }
