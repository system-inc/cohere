package reader

import (
	"example.com/printfimportedextra/api"
	"example.com/printfimportedextra/impl"
)

// init gives api.Tracef a literal that hands its format and values to impl.Printf, so Tracef wraps fmt.Printf in
// reader in this build. It imports only what reader.go does, so reader's imports are the same in every build.
func init() {
	api.Tracef = func(format string, args ...any) { impl.Printf(format, args...) }
}
