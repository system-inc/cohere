package boundaries

import "github.com/system-inc/cohere/internal/lint/rule"

// DependenciesOptions is the subset of upstream's options this port decides, plus the element
// descriptors upstream reads from settings.
//
// # Why the elements live in the options
//
// Upstream reads its elements from `settings["boundaries/elements"]`, beside the rule rather than in
// it, and Base sets different elements in each of the two config blocks that turn the rule on.
// cohere's config has no per-plugin settings, and its overrides carry rules alone, so the only place
// a block-scoped set of elements can travel is inside the rule entry that block writes. That is the
// one deliberate difference from upstream's option schema, and it is an addition rather than a
// reinterpretation: every key upstream writes means here what it means there.
type DependenciesOptions struct {
	// Default decides a dependency no policy matched. Upstream treats anything but "allow" as
	// disallow, so an absent default disallows.
	Default string `json:"default"`

	// Message replaces the default message for every violation a policy does not word itself.
	Message string `json:"message"`

	// Policies are evaluated in order, and the last one to match decides, the way upstream does it.
	Policies []dependencyPolicy `json:"policies"`

	// Elements are upstream's `boundaries/elements` setting: which folders are which layer.
	Elements []elementDescriptor `json:"elements"`

	// CheckAllOrigins, CheckUnknownLocals and CheckInternals are upstream's three widening flags. Each
	// defaults to false, and false is the only value this port implements, so true is refused.
	CheckAllOrigins    *bool `json:"checkAllOrigins"`
	CheckUnknownLocals *bool `json:"checkUnknownLocals"`
	CheckInternals     *bool `json:"checkInternals"`

	// Root is the directory element patterns are relative to, upstream's `rootPath`, which defaults to
	// the directory ESLint runs in. Settled by the decoder from where the config was written, and
	// never read from the config itself.
	Root string `json:"-"`
}

// decide evaluates every policy and reports whether the dependency is refused, and by which policy.
//
// Upstream's order, reproduced exactly: every policy is evaluated and the last one that matches
// decides. Within one policy, disallow is checked first and allow is not checked at all once
// disallow matched. A dependency no policy matched falls to the default. The returned index is -1
// when no policy matched.
func (settings DependenciesOptions) decide(from element, to element) (refused bool, policyIndex int) {
	allowed := false
	matched := -1
	for index := range settings.Policies {
		policy := &settings.Policies[index]
		disallowMatched := false
		for _, entry := range policy.Disallow {
			if policy.entryMatches(entry, from, to) {
				allowed, matched, disallowMatched = false, index, true
				break
			}
		}
		if disallowMatched {
			continue
		}
		for _, entry := range policy.Allow {
			if policy.entryMatches(entry, from, to) {
				allowed, matched = true, index
				break
			}
		}
	}
	if matched < 0 {
		return settings.Default != "allow", -1
	}
	return !allowed, matched
}

// messageFor words a refusal: the policy's own message, else the rule's, else one naming both layers.
func (settings DependenciesOptions) messageFor(policyIndex int, from element, to element) rule.Message {
	if policyIndex >= 0 && settings.Policies[policyIndex].Message != "" {
		return rule.Message{Id: "policyMessage", Description: settings.Policies[policyIndex].Message}
	}
	if settings.Message != "" {
		return rule.Message{Id: "policyMessage", Description: settings.Message}
	}
	if policyIndex < 0 {
		return rule.Message{
			Id: "noPolicyAllows",
			Description: "Nothing in this boundary lets a file in the \"" + from.Type + "\" layer import " +
				"from the \"" + to.Type + "\" layer, and the boundary refuses what it does not allow. A " +
				"layer that reaches a layer it was not given is how the direction the layering promises " +
				"stops being true. Move the shared code where both may reach it, or change the policy.",
		}
	}
	return rule.Message{
		Id: "policyDisallows",
		Description: "A policy in this boundary forbids a file in the \"" + from.Type + "\" layer from " +
			"importing the \"" + to.Type + "\" layer. Move the shared code where both may reach it, or " +
			"change the policy.",
	}
}
