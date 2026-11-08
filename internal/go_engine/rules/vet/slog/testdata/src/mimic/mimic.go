// Package mimic declares functions under slog's names, outside log/slog, which the pass never judges.
package mimic

func Info(message string, arguments ...any) {}

func Log(arguments ...any) {}
