package core

import "github.com/system-inc/cohere/internal/lint/rule"

// NoShadowRestrictedNamesOptions configures which names count as restricted.
//
// Spelled as an allowance rather than as upstream's `reportGlobalThis`, and deliberately. Upstream
// defaults that flag to true, so a Go field named `ReportGlobalThis` would default to false and
// quietly disable a third of the rule for every caller who configured nothing. Inverting it puts
// upstream's default on the zero value, where a struct built by `NoShadowRestrictedNamesOptions{}`
// behaves the way the rule is documented to behave.
type NoShadowRestrictedNamesOptions struct {
	// AllowGlobalThis stops reporting bindings named `globalThis`.
	//
	// It is separable from the rest because it is the newest of the six and the least dangerous:
	// `globalThis` arrived in ES2020, so a codebase predating it can hold a `globalThis` binding
	// that was legal when written, and shadowing it costs less than shadowing `undefined` does.
	// The other five are not configurable, upstream or here.
	AllowGlobalThis bool `json:"-"`
}

// UnmarshalJSON reads upstream's key, `reportGlobalThis`, and stores its inverse.
//
// The field is spelled as an allowance for the reason above, and the config is spelled as upstream
// spells it, so a config written for ESLint means the same thing here. Before this the field had no
// tag, so only the non-upstream `allowGlobalThis` reached it and `{"reportGlobalThis": false}` was
// refused as an unknown key (#4a4yse4).
func (options *NoShadowRestrictedNamesOptions) UnmarshalJSON(raw []byte) error {
	var wire struct {
		ReportGlobalThis *bool `json:"reportGlobalThis"`
	}
	if unmarshalOptionsError := rule.UnmarshalOptions(raw, &wire); unmarshalOptionsError != nil {
		return unmarshalOptionsError
	}
	options.AllowGlobalThis = wire.ReportGlobalThis != nil && !*wire.ReportGlobalThis
	return nil
}
