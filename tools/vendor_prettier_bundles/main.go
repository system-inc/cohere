// Command vendor_prettier_bundles re-copies the Prettier bundles from a built fork into the tree.
//
// The eight bundles under `internal/prettier/bundles` are committed rather than built, because the
// fork gitignores its own `dist` and a submodule pin would give us 9,343 sources and a Node build
// step -- which is the dependency vendoring exists to remove. So this tool is how the vendored copy
// was produced and how it gets moved to a newer fork build.
//
// # The drift this exists to make cheap
//
// Committed build output goes stale silently. Nothing in a `git pull` of the fork updates our copy
// and nothing fails when it diverges, so the only thing standing between a stale bundle and a tree
// formatted slightly wrong forever is somebody remembering to re-copy eight files by hand. Eight
// manual copies is exactly the operation that gets done seven times.
//
// `TestBothDigestPathsAgree` is what catches the divergence; this is what fixes it in one command.
//
// # Why it refuses rather than warns
//
// It writes nothing unless every name in `prettier.BundleFiles` is present in the fork, because a
// partial copy is worse than no copy: seven fresh bundles and one stale one is a binary that formats
// most files correctly, which is the failure that hides. The same rule the engine and the digest
// both hold.
//
// It also refuses a fork whose bundles are stale against their own sources, by resolving through
// `release.ResolveFormatterSource` rather than reading the directory directly. Vendoring a stale
// build is the defect this tool would otherwise automate.
//
//	go run ./tools/vendor_prettier_bundles
//	go run ./tools/vendor_prettier_bundles -dry-run
//
// The fork location follows the same variable the engine and the guard use.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/system-inc/cohere/internal/prettier"
	"github.com/system-inc/cohere/internal/release"
)

// vendoredDirectory is where the committed bundles live, relative to the module root.
//
// It is the directory the `go:embed` directive in `internal/prettier/bundles.go` names. The two have
// to agree, and this tool writing somewhere else would produce a green run and an unchanged binary.
const vendoredDirectory = "internal/prettier/bundles"

func main() {
	dryRun := flag.Bool("dry-run", false, "report what would change without writing")
	flag.Parse()

	if err := run(*dryRun); err != nil {
		fmt.Fprintf(os.Stderr, "vendor_prettier_bundles: %v\n", err)
		os.Exit(1)
	}
}

func run(dryRun bool) error {
	// Resolved through the release guard rather than by reading the directory, so a fork whose
	// bundles are stale against its own sources fails here instead of being vendored.
	source, err := release.ResolveFormatterSource()
	if err != nil {
		return err
	}

	current, err := prettier.Bundles()
	if err != nil {
		return fmt.Errorf("reading the bundles already vendored: %w", err)
	}

	// Read every bundle before writing any, so a fork missing one leaves the tree untouched rather
	// than half-updated. A partial copy is worse than none: seven fresh bundles and one stale is a
	// binary that formats most files correctly, which is the failure that hides.
	incoming := make(map[string][]byte, len(prettier.BundleFiles))
	for _, name := range prettier.BundleFiles {
		content, err := os.ReadFile(filepath.Join(source.Directory, filepath.FromSlash(name)))
		if err != nil {
			return fmt.Errorf("reading %s from the fork at %s: %w", name, source.Directory, err)
		}
		incoming[name] = content
	}

	incomingDigest, err := release.DigestBundleFiles(incoming)
	if err != nil {
		return fmt.Errorf("digesting the fork's bundles: %w", err)
	}

	currentDigest, err := release.DigestBundleFiles(current.Files)
	if err != nil {
		return fmt.Errorf("digesting the vendored bundles: %w", err)
	}

	if incomingDigest == currentDigest {
		fmt.Printf("already current: %s at %s\n", currentDigest[:12], source.Commit)
		return nil
	}

	fmt.Printf("vendored %s -> fork %s (commit %s)\n", currentDigest[:12], incomingDigest[:12], source.Commit)
	for _, name := range prettier.BundleFiles {
		if before, present := current.Files[name]; !present || string(before) != string(incoming[name]) {
			fmt.Printf("  %s\n", name)
		}
	}

	if dryRun {
		fmt.Println("dry run, nothing written")
		return nil
	}

	for _, name := range prettier.BundleFiles {
		destination := filepath.Join(vendoredDirectory, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return fmt.Errorf("making the directory for %s: %w", destination, err)
		}
		if err := os.WriteFile(destination, incoming[name], 0o644); err != nil {
			return fmt.Errorf("writing %s: %w", destination, err)
		}
	}

	fmt.Printf("wrote %d bundles to %s\n", len(prettier.BundleFiles), vendoredDirectory)
	return nil
}
