package cache

import "sync"

// executableAtStart is the running executable, opened as this package initializes, before any of the engine's code
// runs. The identity is hashed through this descriptor, never by path: a `go build` or `go install` that renames a new
// binary over the path while the process runs leaves the old file under the old descriptor, so the process hashes the
// bytes it runs, and never writes the system cache's tables or the project's entries under the new binary's identity.
// The one window left is from exec to this package's initialization, before any of the engine's code has run.
var executableAtStart = openExecutable()

// executableIdentity hashes the executable once per process: it is tens of megabytes, and the code running is what was
// loaded, which the descriptor opened at start reads.
var executableIdentity = sync.OnceValues(executableAtStart.digest)

// ExecutableIdentity is the sha256 of the running executable, memoized for the process. It stands for every rule's
// code and embedded policy at once, so an entry another build of the engine wrote is never read.
func ExecutableIdentity() (string, error) {
	return executableIdentity()
}
