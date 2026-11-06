package micromark

// emailAutolink is one email autolink literal: the closure state upstream keeps in variables, and its
// states, each made once per slot.
type emailAutolink struct {
	self    *Self
	effects *Effects
	ok, nok State

	dot, data bool

	startState, atextState, emailDomainState, emailDomainDotState, emailDomainAfterState State
}

// release zeroes the autolink for its next parse, keeping its bound states.
func (run *emailAutolink) release() {
	*run = emailAutolink{
		startState: run.startState, atextState: run.atextState, emailDomainState: run.emailDomainState,
		emailDomainDotState: run.emailDomainDotState, emailDomainAfterState: run.emailDomainAfterState,
	}
}

// Start of email autolink literal.
func (run *emailAutolink) start(code Code) State {
	if !gfmAutolinkLiteralAtext(code) || !gfmAutolinkLiteralPreviousEmail(run.self.Previous) ||
		gfmAutolinkLiteralPreviousUnbalanced(run.self.Events) {
		return run.nok(code)
	}
	run.effects.Enter(typeLiteralAutolink, nil)
	run.effects.Enter(typeLiteralAutolinkEmail, nil)
	return run.atext(code)
}

// In email atext.
func (run *emailAutolink) atext(code Code) State {
	if gfmAutolinkLiteralAtext(code) {
		run.effects.Consume(code)
		return run.atextState
	}
	if code == CodeAtSign {
		run.effects.Consume(code)
		return run.emailDomainState
	}
	return run.nok(code)
}

// In email domain.
//
// The reference code is a bit overly complex as it handles the `@`, of which
// there may be just one.
// Source: <https://github.com/github/cmark-gfm/blob/ef1cfcb/extensions/autolink.c#L318>
func (run *emailAutolink) emailDomain(code Code) State {
	// Dot followed by alphanumerical (not `-` or `_`).
	if code == CodeDot {
		return run.effects.Check(
			gfmAutolinkLiteralEmailDomainDotTrail,
			run.emailDomainAfterState,
			run.emailDomainDotState,
		)(code)
	}

	// Alphanumerical, `-`, and `_`.
	if code == CodeDash || code == CodeUnderscore || asciiAlphanumeric(code) {
		run.data = true
		run.effects.Consume(code)
		return run.emailDomainState
	}

	// To do: `/` if xmpp.

	// Note: normally we’d truncate trailing punctuation from the link.
	// However, email autolink literals cannot contain any of those markers,
	// except for `.`, but that can only occur if it isn’t trailing.
	// So we can ignore truncating!
	return run.emailDomainAfter(code)
}

// In email domain, on dot that is not a trail.
func (run *emailAutolink) emailDomainDot(code Code) State {
	run.effects.Consume(code)
	run.dot = true
	return run.emailDomainState
}

// After email domain.
func (run *emailAutolink) emailDomainAfter(code Code) State {
	// Domain must not be empty, must include a dot, and must end in alphabetical.
	// Source: <https://github.com/github/cmark-gfm/blob/ef1cfcb/extensions/autolink.c#L332>.
	if run.data && run.dot && asciiAlpha(run.self.Previous) {
		run.effects.Exit(typeLiteralAutolinkEmail)
		run.effects.Exit(typeLiteralAutolink)
		return run.ok(code)
	}
	return run.nok(code)
}
