package differences

// The functions here hold the replay to the push's seen set. The push judges Alpha first, and with it Fresh, which
// Alpha calls, so when it reaches Gamma, which calls Fresh through a method value, it holds Fresh's fact. A resolver
// asked of Fresh and Alpha first, then of BetaInner, replays the push from the top: it holds Alpha's verdict already,
// so it does not judge Alpha again, but it must still leave Fresh seen, as judging Alpha would have, or Gamma would
// read no fact on Fresh and return what may be nil.

// Alpha calls Fresh directly, so the push judges Fresh with it.
func Alpha(node *Node) *Node { // want Alpha:`nilness: \[\{MaybeNil NeverNil\}\]`
	return node.Fresh()
}

// Gamma calls Fresh through a method value, whose wrapper the push reads Fresh's fact for.
func Gamma(node *Node) *Node { // want Gamma:`nilness: \[\{MaybeNil NeverNil\}\]`
	fresh := node.Fresh
	return fresh()
}

// BetaOuter and BetaInner call each other, so asking of BetaInner, after Alpha and before Gamma, replays the push.
func BetaOuter(node *Node) *Node { // want BetaOuter:`nilness: \[\{MaybeNil NeverNil\}\]`
	if node != nil {
		node.Next = BetaInner(node.Next)
	}
	return &Node{}
}

// BetaInner returns what BetaOuter returns, which the push, reaching it from BetaOuter, does not know yet.
func BetaInner(node *Node) *Node {
	return BetaOuter(node)
}

// Fresh is never nil, and declared last, so the push judges it through Alpha before it reaches it as a root.
func (node *Node) Fresh() *Node { // want Fresh:`nilness: \[\{MaybeNil NeverNil\}\]`
	return &Node{}
}
