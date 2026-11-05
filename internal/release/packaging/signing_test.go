package release

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// VerifySignature is the release's only check that a signature took, and nothing reaches it without a
// Developer ID certificate, so a broken check would first show itself on a release runner. That
// happened: a rename turned its `codesign --verify` into `codesign --cohere`, which codesign refuses as
// an unknown option, and every signed release would have failed after signing its first binary. These
// run the real codesign on binaries macOS already has, both ways: one Apple signed, which must pass,
// and the same bytes re-signed ad hoc, which must be refused as ad hoc rather than as anything else.

func TestVerifySignaturePassesARealSignature(test *testing.T) {
	test.Parallel()
	if runtime.GOOS != "darwin" {
		test.Skip("codesign exists only on macOS")
	}

	if verifySignatureError := VerifySignature("/usr/bin/true"); verifySignatureError != nil {
		test.Fatalf("an Apple signed binary was refused: %v", verifySignatureError)
	}
}

func TestVerifySignatureRefusesAnAdHocSignature(test *testing.T) {
	test.Parallel()
	if runtime.GOOS != "darwin" {
		test.Skip("codesign exists only on macOS")
	}

	original, err := os.ReadFile("/usr/bin/true")
	if err != nil {
		test.Fatal(err)
	}
	binaryPath := filepath.Join(test.TempDir(), "true")
	if writeError := os.WriteFile(binaryPath, original, 0o755); writeError != nil {
		test.Fatal(writeError)
	}
	// Ad hoc is what Go's linker leaves on every arm64 binary, so it is what a signing step that did
	// nothing looks like.
	if output, outputError := exec.Command("codesign", "--force", "--sign", "-", binaryPath).CombinedOutput(); outputError != nil {
		test.Fatalf("signing the copy ad hoc: %v\n%s", outputError, output)
	}

	err = VerifySignature(binaryPath)
	if err == nil {
		test.Fatal("an ad hoc signature was accepted")
	}
	if !strings.Contains(err.Error(), "ad-hoc signature") {
		test.Fatalf("refused, but not as ad hoc, so the check never read the signature: %v", err)
	}
}

// The verdict is read from notarytool's JSON, never from its exit code, so these hold the reading to
// Apple's own shape: an Accepted answer, an Invalid one, and output that carries no status at all.

func TestReadNotaryVerdictReadsTheStatus(test *testing.T) {
	test.Parallel()

	accepted, err := readNotaryVerdict([]byte(`{"id":"2efe2717-52ef-43a5-96dc-0797e4ca1041","message":"Processing complete","status":"Accepted"}`))
	if err != nil || accepted.Status != "Accepted" || accepted.Identifier != "2efe2717-52ef-43a5-96dc-0797e4ca1041" {
		test.Fatalf("an Accepted answer read as %+v, %v", accepted, err)
	}

	invalid, err := readNotaryVerdict([]byte(`{"id":"8f1c2c55-0f5b-4a4e-9a9d-3d1f2b6a7e10","message":"Processing complete","status":"Invalid"}`))
	if err != nil || invalid.Status != "Invalid" {
		test.Fatalf("an Invalid answer read as %+v, %v", invalid, err)
	}

	for _, output := range []string{``, `not json`, `{"id":"8f1c2c55-0f5b-4a4e-9a9d-3d1f2b6a7e10"}`} {
		if _, readNotaryVerdictError := readNotaryVerdict([]byte(output)); readNotaryVerdictError == nil {
			test.Errorf("%q was read as a verdict", output)
		}
	}
}

func TestNotaryCredentialsNameTheKeychainOnlyWhenThereIsOne(test *testing.T) {
	test.Parallel()

	withKeychain := notaryCredentials(Signing{KeychainProfile: "cohere-notary", NotaryKeychain: "/runner/build.keychain-db"})
	if strings.Join(withKeychain, " ") != "--keychain-profile cohere-notary --keychain /runner/build.keychain-db" {
		test.Fatalf("with a keychain: %q", withKeychain)
	}
	withoutKeychain := notaryCredentials(Signing{KeychainProfile: "cohere-notary"})
	if strings.Join(withoutKeychain, " ") != "--keychain-profile cohere-notary" {
		test.Fatalf("without a keychain: %q", withoutKeychain)
	}
}
