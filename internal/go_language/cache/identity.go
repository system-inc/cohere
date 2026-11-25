package cache

// executableAtStart is the running executable, opened as this package initializes, before any of cohere-go's code
// runs. The identity is hashed through this descriptor, never by path: a `go build` or `go install` that renames a new
// binary over the path while the process runs leaves the old file under the old descriptor, so the process hashes the
// bytes it runs, and never writes the system cache's tables or the project's entries under the new binary's identity.
// The one window left is from exec to this package's initialization, before any of cohere-go's code has run.
var executableAtStart = openExecutable()

// executableIdentity hashes the executable once per process: it is tens of megabytes, and the code running is what was
// loaded, which the descriptor opened at start reads.
var executableIdentity = newExecutableDigest(executableAtStart, nil)

// StartExecutableIdentity begins hashing the running executable on a goroutine of its own, for a run that will key by
// cohere-go's identity, so the digest is ready, or nearly, by the time its first key asks (executableDigest). It
// returns at once, and starting twice hashes once.
func StartExecutableIdentity() {
	executableIdentity.start()
}

// ExecutableIdentity is the sha256 of the running executable, memoized for the process. It stands for every rule's
// code and embedded policy at once, so an entry another build of cohere-go wrote is never read.
func ExecutableIdentity() (string, error) {
	return executableIdentity.get()
}
