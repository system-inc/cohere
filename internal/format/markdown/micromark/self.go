package micromark

// Self is what a construct's tokenize receives as `this`: the context, or, under interrupt, an object
// whose prototype is the context with `interrupt: true` set on it.
//
// Reads go through to the live context. interrupt is the one field upstream ever writes through `this`,
// so it is the one field a view can shadow.
type Self struct {
	*TokenizeContext
	isView        bool
	viewInterrupt bool
}

// IsInterrupt reads `this.interrupt`.
func (view *Self) IsInterrupt() bool {
	if view.isView {
		return view.viewInterrupt
	}
	return view.TokenizeContext.Interrupt
}

// SetInterrupt writes `this.interrupt`, onto the view when there is one, as a property assignment would.
func (view *Self) SetInterrupt(value bool) {
	if view.isView {
		view.viewInterrupt = value
		return
	}
	view.TokenizeContext.Interrupt = value
}
