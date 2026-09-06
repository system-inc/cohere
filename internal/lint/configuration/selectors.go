package configuration

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// ValidateSelectors proves that every override selector reaches at least one file the linter can
// visit.
//
// This cannot run during Load: a configuration file describes patterns, while only the type graph
// knows the exact project-file population those patterns are meant to select. Keeping the check on
// Config lets the command join those two facts before a rule runs and prevents a misspelled glob
// from turning an override into a clean-looking no-op.
//
// Each pattern is checked independently rather than treating an override's files array as one OR.
// If one of two patterns is stale, the other still makes the block run somewhere and a block-level
// check passes while half the intended scope is absent. A pattern that reaches only globally ignored
// files is also a no-op, so ignored files are not part of the population.
func (c *Config) ValidateSelectors(fileNames []string) error {
	lintable := make([]string, 0, len(fileNames))
	for _, fileName := range fileNames {
		relative := c.relativePath(fileName)
		if MatchAny(c.IgnorePatterns, relative) {
			continue
		}
		lintable = append(lintable, relative)
	}

	type miss struct {
		override int
		pattern  string
		empty    bool
	}
	var misses []miss
	for overrideIndex, override := range c.Overrides {
		if len(override.Files) == 0 {
			misses = append(misses, miss{override: overrideIndex, empty: true})
			continue
		}
		for _, pattern := range override.Files {
			matched := false
			for _, fileName := range lintable {
				if Match(pattern, fileName) {
					matched = true
					break
				}
			}
			if !matched {
				misses = append(misses, miss{override: overrideIndex, pattern: pattern})
			}
		}
	}
	if len(misses) == 0 {
		return nil
	}

	sort.Slice(misses, func(first, second int) bool {
		if misses[first].override != misses[second].override {
			return misses[first].override < misses[second].override
		}
		return misses[first].pattern < misses[second].pattern
	})
	descriptions := make([]string, 0, len(misses))
	for _, missing := range misses {
		if missing.empty {
			descriptions = append(descriptions, fmt.Sprintf("override %d with no files patterns", missing.override))
			continue
		}
		descriptions = append(descriptions, fmt.Sprintf(
			"override %d pattern %s", missing.override, strconv.Quote(missing.pattern)))
	}

	return fmt.Errorf(
		"lint config declares %s, which matched none of the %d non-ignored project files; "+
			"a selector that reaches no files makes its rules vacuous",
		strings.Join(descriptions, ", "), len(lintable))
}
