package core

// arrowBodyStyleModeError names an unrecognized mode and the three that are recognized.
type arrowBodyStyleModeError struct{ mode string }

func (modeError *arrowBodyStyleModeError) Error() string {
	return "arrow-body-style: unknown mode " + modeError.mode +
		", wanted one of as-needed, always, never"
}
