package typescript

// BanTsCommentOptions configures which directives report and what an acceptable description is.
//
// typescript-eslint's five keys and its `defaultOptions`. A default that binds and does nothing is
// the failure this rule is most exposed to, so the corpus's default-options rows pin every one.
type BanTsCommentOptions struct {
	// TsExpectError defaults to allow-with-description, the only directive not defaulting to a
	// boolean. This is why a bare `"error"` config reports an undescribed `@ts-expect-error` and
	// stays silent on a described one.
	TsExpectError BanTsCommentSetting

	// TsIgnore defaults to banned, and it is the only directive offering a repair.
	TsIgnore BanTsCommentSetting

	// TsNoCheck defaults to banned.
	TsNoCheck BanTsCommentSetting

	// TsCheck defaults to allowed. `@ts-check` turns checking on rather than off, so banning it by
	// default would be backwards, and this is the one default a reader is likely to guess wrong.
	TsCheck BanTsCommentSetting

	// MinimumDescriptionLength defaults to 3, counted in graphemes of the trimmed description.
	MinimumDescriptionLength int
}

// settingFor is upstream's `options[fullDirective]`, mapping a directive name onto its setting.
//
// The fallback is unreachable, because `findBanTsCommentDirective` returns only the four names the
// switch covers, and a mutant flipping it to a ban survives for that reason. Go requires a return,
// and the allowing setting is the one that cannot report on input this rule never produces.
func (options BanTsCommentOptions) settingFor(directive string) BanTsCommentSetting {
	switch directive {
	case banTsCommentIgnore:
		return options.TsIgnore
	case banTsCommentCheck:
		return options.TsCheck
	case banTsCommentNoCheck:
		return options.TsNoCheck
	case banTsCommentExpectError:
		return options.TsExpectError
	}
	return BanTsCommentSetting{Kind: BanTsCommentBoolean, Banned: false}
}
