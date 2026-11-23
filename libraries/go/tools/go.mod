module golang.org/x/tools

go 1.26.0

require (
	github.com/google/go-cmp v0.6.0
	github.com/yuin/goldmark v1.4.13
	golang.org/x/mod v0.41.0
	golang.org/x/net v0.59.0
	golang.org/x/sync v0.23.0
	golang.org/x/telemetry v0.0.0-20260908163034-4bcc4b2ee518
)

require golang.org/x/sys v0.48.0 // indirect

// cohere: the fork of go/types every copied import of go/types names instead, which cohere's go.mod
// replaces with libraries/go/go-types (tools/go_libraries).
require github.com/system-inc/go-types v0.0.0
