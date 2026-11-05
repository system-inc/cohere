package release

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Signing describes how the macOS binaries are signed and notarized.
//
// What Gatekeeper actually does, measured rather than assumed, because the answer decides how much
// of this is required:
//
//   - An unsigned binary with no quarantine attribute runs normally. This is what an `npm install`
//     produces today: package managers extract tarballs without setting `com.apple.quarantine`, so
//     a consumer installing cohere from the registry is not blocked.
//   - The same binary with the quarantine attribute set is killed by the kernel: exit 137, SIGKILL,
//     and zero bytes on both stdout and stderr. Measured directly on a staged darwin/arm64 build.
//
// That second case is the reason to sign. It is reached whenever the binary travels as a file
// rather than as a package — a release asset downloaded from a browser or curl, a tarball someone
// forwards, a binary copied out of CI — and it fails in the worst available way: silently, with no
// output to explain it. A developer seeing an empty result would reasonably read it as a clean run,
// which is precisely the confusion cohere exists to eliminate.
//
// So signing is not optional-but-nice. It converts a silent kill into a normal execution, and the
// launcher's signal handling converts anything left over into a loud non-zero rather than a green
// nothing.
type Signing struct {
	// Identity is the codesigning identity, as `security find-identity -v -p codesigning` prints
	// it. Notarization requires a "Developer ID Application" certificate specifically; an "Apple
	// Development" certificate signs successfully and is then rejected by the notary service, which
	// is a failure that only appears at the end of a release.
	//
	// Measured rather than assumed, by signing a real staged binary with the Apple Development
	// identity on this machine and assessing it read-only:
	//
	//	unsigned            Signature=adhoc                  spctl: rejected
	//	Apple Development   Authority=Apple Development ...   spctl: rejected
	//
	// So signing with the wrong certificate type changes the signature and not the verdict. It is
	// worth stating because the intermediate state looks like progress: `codesign --verify` passes,
	// the binary carries a real Apple chain and a TeamIdentifier, and Gatekeeper still refuses it.
	// A release that signed with what was available and stopped there would read as done.
	Identity string

	// KeychainProfile is the `notarytool` profile holding the notary credentials, stored with
	// `xcrun notarytool store-credentials`. Credentials are never passed as arguments, because a
	// command line is visible to every process on the machine and ends up in CI logs.
	KeychainProfile string

	// NotaryKeychain is the path of the keychain file holding KeychainProfile, passed to every
	// `notarytool` call. A release runner keeps its profile in a keychain made for the run, and
	// notarytool takes a keychain by path, never by name: `--keychain build.keychain` is refused as a
	// file that doesn't exist. Empty means the user's own keychains.
	NotaryKeychain string
}

// IsConfigured reports whether signing can run.
func (signing Signing) IsConfigured() bool {
	return signing.Identity != ""
}

// CanNotarize reports whether notarization can run.
//
// Signing and notarizing are separate steps with separate credentials, and a release can legitimately
// do the first without the second — a signed binary still fails a quarantined launch until the
// notary service has seen it, but it fails differently and it is a strictly better state than
// unsigned. Treating them as one step would mean losing the signature when only the notary
// credentials are missing.
func (signing Signing) CanNotarize() bool {
	return signing.IsConfigured() && signing.KeychainProfile != ""
}

// Sign codesigns a macOS binary in place.
//
// The signature is applied with a hardened runtime and a timestamp because the notary service
// rejects binaries without either, and it rejects them after the upload rather than before — so
// omitting these turns a fast local error into a slow remote one.
func Sign(binaryPath string, signing Signing) error {
	if !signing.IsConfigured() {
		return fmt.Errorf("no signing identity configured")
	}

	command := exec.Command(
		"codesign",
		"--sign", signing.Identity,
		// Replace any existing signature. Go's linker applies an ad-hoc signature on arm64, so
		// every darwin binary arrives here already signed and an unforced codesign fails.
		"--force",
		// Required by the notary service.
		"--options", "runtime",
		"--timestamp",
		binaryPath,
	)
	command.Stderr = os.Stderr

	if runError := command.Run(); runError != nil {
		return fmt.Errorf("codesigning %s: %w", binaryPath, runError)
	}

	return VerifySignature(binaryPath)
}

// VerifySignature confirms a binary carries a real signature.
//
// This runs after every signing rather than trusting codesign's exit code, for the same reason the
// build checks that a binary exists after `go build` reports success: the artifact is the truth. A
// signature that did not take produces a binary that ships, installs, and is killed on the first
// machine that receives it through a quarantining path.
func VerifySignature(binaryPath string) error {
	command := exec.Command("codesign", "--verify", "--strict", "--verbose=2", binaryPath)

	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("the signature on %s did not verify: %w\n%s", binaryPath, err, output)
	}

	// An ad-hoc signature verifies successfully and is worth nothing to Gatekeeper. Go's linker
	// applies one to every arm64 binary, so a signing step that silently did nothing leaves a
	// binary that passes `codesign --verify` and is still killed on a quarantined launch. Checking
	// for it is what separates "verified" from "verified as actually signed by us".
	details, err := exec.Command("codesign", "--display", "--verbose=2", binaryPath).CombinedOutput()
	if err != nil {
		return fmt.Errorf("reading the signature on %s: %w", binaryPath, err)
	}
	if strings.Contains(string(details), "Signature=adhoc") {
		return fmt.Errorf(
			"%s carries only an ad-hoc signature, which Gatekeeper does not accept — the signing step did not take",
			binaryPath,
		)
	}

	return nil
}

// Notarize submits a signed binary to Apple and waits for the verdict, which must be Accepted.
//
// A binary is submitted inside a zip because the notary service takes archives rather than bare
// executables. The verdict is waited on rather than polled later, because a release that continues
// past an unfinished submission can publish a binary the service later rejects. And it is read,
// not inferred from the exit code: notarytool can finish a submission whose status is Invalid, and
// a release that took that for a pass would print "signed and notarized" over a binary Apple refused.
//
// Stapling is deliberately not attempted. A ticket can only be stapled to a bundle, a disk image,
// or an installer package, never to a bare executable, so a Mach-O binary is validated by an online
// check against Apple's service on first launch instead. This is a real limitation rather than an
// oversight: a machine that is entirely offline the first time it runs a quarantined cohere will be
// blocked. The npm install path does not set quarantine at all, so this does not affect it.
//
// Each submission is announced to progress as it starts and as it is accepted, since it waits minutes on Apple's
// service; a nil progress announces nothing, and an announcement that cannot be written stops the release.
func Notarize(binaryPath string, signing Signing, progress io.Writer) error {
	if !signing.CanNotarize() {
		return fmt.Errorf("no notarytool keychain profile configured")
	}

	directory, err := os.MkdirTemp("", "cohere-notarize-")
	if err != nil {
		return fmt.Errorf("making a directory for the notary archive: %w", err)
	}
	// Ignored: the directory is under the system's temporary directory, and an archive left there is a copy of a
	// signed binary that nothing reads again.
	defer os.RemoveAll(directory)
	zipPath := filepath.Join(directory, filepath.Base(binaryPath)+".zip")
	// ditto rather than zip, as Apple's notarization guide uses: it keeps the signature's extended
	// attributes, which zip can drop.
	if output, outputError := exec.Command(
		"ditto",
		"-c",
		"-k",
		"--keepParent",
		binaryPath,
		zipPath,
	).CombinedOutput(); outputError != nil {
		return fmt.Errorf("archiving %s for the notary service: %w\n%s", binaryPath, outputError, output)
	}

	if announceError := announce(progress, "notarizing %s\n", binaryPath); announceError != nil {
		return announceError
	}
	arguments := append([]string{"notarytool", "submit", zipPath},
		notaryCredentials(signing)...)
	// Without --wait the command returns as soon as the upload finishes, and a release would
	// continue on a submission that has not been judged yet.
	arguments = append(arguments, "--wait", "--output-format", "json")
	command := exec.Command("xcrun", arguments...)
	command.Stderr = os.Stderr
	output, err := command.Output()
	if err != nil {
		return fmt.Errorf("notarizing %s: %w\n%s", binaryPath, err, output)
	}

	verdict, err := readNotaryVerdict(output)
	if err != nil {
		return fmt.Errorf("notarizing %s: %w", binaryPath, err)
	}
	if verdict.Status != "Accepted" {
		// Apple's log names each problem by file and reason, which is the only place the cause is.
		logArguments := append([]string{"notarytool", "log", verdict.Identifier}, notaryCredentials(signing)...)
		notaryLog, logError := exec.Command("xcrun", logArguments...).CombinedOutput()
		if logError != nil {
			return fmt.Errorf("the notary service answered %q for %s (submission %s), and its log was not read: %w\n%s",
				verdict.Status, binaryPath, verdict.Identifier, logError, notaryLog)
		}
		return fmt.Errorf("the notary service answered %q for %s (submission %s)\n%s",
			verdict.Status, binaryPath, verdict.Identifier, notaryLog)
	}
	return announce(progress, "notarized %s: Accepted, submission %s\n", binaryPath, verdict.Identifier)
}

// announce writes one line of a release's progress, or nothing to a nil progress.
func announce(progress io.Writer, format string, arguments ...any) error {
	if progress == nil {
		return nil
	}
	if _, writeError := fmt.Fprintf(progress, format, arguments...); writeError != nil {
		return fmt.Errorf("announcing the notarization: %w", writeError)
	}
	return nil
}

// notaryCredentials are the arguments that name the stored profile, and its keychain when it has one.
func notaryCredentials(signing Signing) []string {
	arguments := []string{"--keychain-profile", signing.KeychainProfile}
	if signing.NotaryKeychain != "" {
		arguments = append(arguments, "--keychain", signing.NotaryKeychain)
	}
	return arguments
}

// notaryVerdict is the part of `notarytool submit --output-format json` a release reads.
type notaryVerdict struct {
	Identifier string `json:"id"`
	Status     string `json:"status"`
}

// readNotaryVerdict reads notarytool's answer. One with no status is refused rather than read as
// anything, since the status is the whole point of waiting for it.
func readNotaryVerdict(output []byte) (notaryVerdict, error) {
	var verdict notaryVerdict
	if decodeError := json.Unmarshal(output, &verdict); decodeError != nil {
		return notaryVerdict{}, fmt.Errorf("reading the notary service's answer: %w\n%s", decodeError, output)
	}
	if verdict.Status == "" {
		return notaryVerdict{}, fmt.Errorf("the notary service's answer carries no status\n%s", output)
	}
	return verdict, nil
}

// DescribeSigningState explains what signing did or did not happen, for the release summary.
//
// An unsigned release is allowed, because signing requires credentials a contributor may not have
// and blocking every local staging on them would be worse. What is not allowed is an unsigned
// release that looks signed, so the state prints either way and says what the consequence is.
func DescribeSigningState(signing Signing) string {
	switch {
	case signing.CanNotarize():
		return "macOS binaries: signed and notarized"
	case signing.IsConfigured():
		return "macOS binaries: signed, NOT notarized — a quarantined launch will still be blocked until the notary service has seen this build"
	default:
		return "macOS binaries: NOT signed — fine for an npm install, but a copied or downloaded binary will be killed by Gatekeeper with no output"
	}
}
