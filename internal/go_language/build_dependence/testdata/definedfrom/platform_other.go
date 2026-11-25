//go:build !linux

package definedfrom

// platformExtra has no field everywhere but linux.
type platformExtra struct{}

// platformPart embeds nothing everywhere but linux, so it has no method.
type platformPart struct{}
