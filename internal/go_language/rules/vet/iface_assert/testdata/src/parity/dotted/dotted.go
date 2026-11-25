// Package dotted names io's interfaces through a dot import, in a package of its own, since a dot import of io
// would clash with parity's names.
package dotted

import . "io"

// Dot names io's interfaces through a dot import, which the message qualifies by package all the same.
func Dot(closer Closer, writer Writer) {
	_ = closer.(interface{ Close() int }) // finding: Close
	_ = writer.(interface{ Write() })     // finding: Write
	_ = writer.(WriteCloser)              // clean
	switch closer.(type) {
	case interface{ Close() }: // finding: Close
	case ReadCloser:
	}
}
