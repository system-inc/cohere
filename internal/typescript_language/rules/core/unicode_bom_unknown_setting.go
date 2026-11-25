package core

import "strings"

// unicodeBomUnknownSetting names the bad value rather than only saying the shape was wrong.
type unicodeBomUnknownSetting struct {
	configured string
}

func (setting *unicodeBomUnknownSetting) Error() string {
	// Go whitespace: the bad option value echoed in cohere's own error message, which decides nothing.
	return "unicode-bom takes \"always\" or \"never\", got " + strings.TrimSpace(setting.configured)
}
