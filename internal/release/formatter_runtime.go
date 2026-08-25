package release

import (
	"fmt"

	"github.com/system-inc/verify/internal/prettier"
)

// VerifyEmbeddedBundles checks that the bundles this binary carries are the ones it was stamped
// with, and says which copy it actually looked at.
//
// This is the runtime half of the vendoring. The build stamps a digest over the bytes it embedded;
// nothing read that stamp back, which made it a claim rather than a check -- and a stamp nobody
// verifies is the same shape as a guard vouching for a directory nothing loads from.
//
// It does not check freshness, and that absence is the design rather than an omission. Embedded
// bytes cannot be stale, they can only be old: there is no working tree to compare them against and
// no modification time that is not the build's own. A staleness refusal here would be a check with
// no artifact behind it. What can go wrong with embedded bytes is that they are not the bytes the
// stamp names, and that is exactly what this detects.
//
// A binary with no stamp reports that it cannot say, rather than passing. Those are different facts
// and collapsing them would let an unstamped build read as verified.
func VerifyEmbeddedBundles() error {
	provenance := Current()

	source, err := prettier.Bundles()
	if err != nil {
		return fmt.Errorf("reading this binary's Prettier bundles: %w", err)
	}

	// The override means the bytes came from a checkout this build knows nothing about, so there is
	// no stamp they could be expected to match. Saying so is the honest answer; comparing them
	// against the embedded stamp would fail on a correct developer setup.
	if source.Origin == prettier.Disk {
		return fmt.Errorf(
			"the bundles came from %s because %s is set, so the stamp in this binary does not describe them",
			source.Path, FormatterForkPathVariable,
		)
	}

	if provenance.FormatterDigest == "" {
		return fmt.Errorf(
			"this binary carries no formatter digest, so its embedded bundles cannot be checked against anything",
		)
	}

	digest, err := DigestBundleFiles(source.Files)
	if err != nil {
		return fmt.Errorf("digesting this binary's embedded bundles: %w", err)
	}

	if digest != provenance.FormatterDigest {
		return fmt.Errorf(
			"this binary's embedded bundles do not match its stamp:\n  stamped %s\n  actual  %s\nThe binary was built from different bytes than the ones it carries",
			provenance.FormatterDigest, digest,
		)
	}

	return nil
}
