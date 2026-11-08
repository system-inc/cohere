package parity

import (
	"context"

	. "log/slog"
)

// A dot import names slog's functions bare, and they are still slog's: each misplaces a key, drops a final value, or
// pairs.
func DotImported() {
	Info("m", 1, 2)
	Warn("m", "k")
	Error("m", "k", 1)
	DebugContext(context.Background(), "m", 1, 2)
	Log(context.Background(), LevelWarn, "m", "k")
	_ = Group("g", Int("k", 1), 1)
	Default().Info("m", "k")
	(*Logger).Warn(Default(), "m", 1, 2)
	var record Record
	record.Add("k")
}
