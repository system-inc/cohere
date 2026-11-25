package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"testing"
)

// The engine's identity is the sha256 of the running executable's bytes, the same on every call.
func TestExecutableIdentityIsTheExecutablesSha256(test *testing.T) {
	test.Parallel()

	executable, executableError := os.Executable()
	if executableError != nil {
		test.Fatal(executableError)
	}
	contents, readError := os.ReadFile(executable)
	if readError != nil {
		test.Fatal(readError)
	}
	sum := sha256.Sum256(contents)
	want := hex.EncodeToString(sum[:])
	for range 2 {
		identity, identityError := ExecutableIdentity()
		if identityError != nil {
			test.Fatal(identityError)
		}
		if identity != want {
			test.Errorf("want %s, got %s", want, identity)
		}
	}
}
