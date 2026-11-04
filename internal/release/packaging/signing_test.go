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

func TestVerifySignaturePassesARealSignature(t *testing.T) {
	t.Parallel()
	if runtime.GOOS != "darwin" {
		t.Skip("codesign exists only on macOS")
	}

	if err := VerifySignature("/usr/bin/true"); err != nil {
		t.Fatalf("an Apple signed binary was refused: %v", err)
	}
}

func TestVerifySignatureRefusesAnAdHocSignature(t *testing.T) {
	t.Parallel()
	if runtime.GOOS != "darwin" {
		t.Skip("codesign exists only on macOS")
	}

	original, err := os.ReadFile("/usr/bin/true")
	if err != nil {
		t.Fatal(err)
	}
	binaryPath := filepath.Join(t.TempDir(), "true")
	if err := os.WriteFile(binaryPath, original, 0o755); err != nil {
		t.Fatal(err)
	}
	// Ad hoc is what Go's linker leaves on every arm64 binary, so it is what a signing step that did
	// nothing looks like.
	if output, err := exec.Command("codesign", "--force", "--sign", "-", binaryPath).CombinedOutput(); err != nil {
		t.Fatalf("signing the copy ad hoc: %v\n%s", err, output)
	}

	err = VerifySignature(binaryPath)
	if err == nil {
		t.Fatal("an ad hoc signature was accepted")
	}
	if !strings.Contains(err.Error(), "ad-hoc signature") {
		t.Fatalf("refused, but not as ad hoc, so the check never read the signature: %v", err)
	}
}

// The verdict is read from notarytool's JSON, never from its exit code, so these hold the reading to
// Apple's own shape: an Accepted answer, an Invalid one, and output that carries no status at all.

func TestReadNotaryVerdictReadsTheStatus(t *testing.T) {
	t.Parallel()

	accepted, err := readNotaryVerdict([]byte(`{"id":"2efe2717-52ef-43a5-96dc-0797e4ca1041","message":"Processing complete","status":"Accepted"}`))
	if err != nil || accepted.Status != "Accepted" || accepted.Identifier != "2efe2717-52ef-43a5-96dc-0797e4ca1041" {
		t.Fatalf("an Accepted answer read as %+v, %v", accepted, err)
	}

	invalid, err := readNotaryVerdict([]byte(`{"id":"8f1c2c55-0f5b-4a4e-9a9d-3d1f2b6a7e10","message":"Processing complete","status":"Invalid"}`))
	if err != nil || invalid.Status != "Invalid" {
		t.Fatalf("an Invalid answer read as %+v, %v", invalid, err)
	}

	for _, output := range []string{``, `not json`, `{"id":"8f1c2c55-0f5b-4a4e-9a9d-3d1f2b6a7e10"}`} {
		if _, err := readNotaryVerdict([]byte(output)); err == nil {
			t.Errorf("%q was read as a verdict", output)
		}
	}
}

func TestNotaryCredentialsNameTheKeychainOnlyWhenThereIsOne(t *testing.T) {
	t.Parallel()

	withKeychain := notaryCredentials(Signing{KeychainProfile: "cohere-notary", NotaryKeychain: "/runner/build.keychain-db"})
	if strings.Join(withKeychain, " ") != "--keychain-profile cohere-notary --keychain /runner/build.keychain-db" {
		t.Fatalf("with a keychain: %q", withKeychain)
	}
	withoutKeychain := notaryCredentials(Signing{KeychainProfile: "cohere-notary"})
	if strings.Join(withoutKeychain, " ") != "--keychain-profile cohere-notary" {
		t.Fatalf("without a keychain: %q", withoutKeychain)
	}
}
