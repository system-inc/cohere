package release

import (
	"encoding/json"
	"fmt"
)

// DispatcherPackageName is the package a consumer actually installs.
const DispatcherPackageName = "verify"

// RepositoryURL is where the source lives, recorded in every published package.
const RepositoryURL = "https://github.com/system-inc/verify"

// PlatformManifest is the package.json for one platform's binary package.
//
// The `os` and `cpu` fields are the entire mechanism. npm and pnpm read them to decide which
// optional dependency to install on this machine, so a package whose fields disagree with its
// contents installs on the wrong platform and produces an exec-format error, or installs nowhere
// and looks like an unsupported platform. They are generated from the same target as the binary,
// rather than written by hand, so the two cannot drift.
func PlatformManifest(target Target, version string) ([]byte, error) {
	manifest := map[string]any{
		"name":        target.PackageName(),
		"version":     version,
		"description": fmt.Sprintf("The verify binary for %s.", target),
		"license":     "MIT",
		"repository":  map[string]string{"type": "git", "url": RepositoryURL},

		"os":  []string{NpmOperatingSystem(target.GoOperatingSystem)},
		"cpu": []string{NpmArchitecture(target.GoArchitecture)},

		// Only the binary ships. Without this npm includes whatever is in the directory, and a
		// stray file in a 15 MB package is the kind of thing nobody notices until it is a secret.
		"files": []string{"bin/"},

		// Deliberately no `bin` field. The platform packages are data, not commands: the dispatcher
		// owns the `verify` name, and a second package claiming it would race for the same link in
		// `node_modules/.bin` and win or lose depending on install order.
	}
	return marshalManifest(manifest)
}

// DispatcherManifest is the package.json for the package a consumer installs.
//
// Every platform package is an optional dependency. That is what makes an install on an unsupported
// platform succeed with a missing binary rather than fail outright — which sounds worse and is
// better, because the failure then happens at `verify --version` with a message naming the
// platform, instead of inside a package manager's dependency resolution where it says nothing
// useful. The loud failure is the dispatcher's job and it is a better place to fail from.
func DispatcherManifest(version string) ([]byte, error) {
	optionalDependencies := map[string]string{}
	for _, target := range Targets {
		// Pinned exactly, not with a range. The dispatcher and the binary are one artifact split
		// across packages for distribution, so a resolver free to pick a different patch of the
		// binary could pair a dispatcher with a binary built from other rules.
		optionalDependencies[target.PackageName()] = version
	}

	manifest := map[string]any{
		"name":        DispatcherPackageName,
		"version":     version,
		"description": "Type-check, lint, fix, and format a TypeScript codebase in one process.",
		"license":     "MIT",
		"repository":  map[string]string{"type": "git", "url": RepositoryURL},

		"bin":   map[string]string{"verify": "bin/verify"},
		"files": []string{"bin/"},

		"optionalDependencies": optionalDependencies,
	}
	return marshalManifest(manifest)
}

// marshalManifest renders a manifest the way a formatter would leave it.
//
// Two-space indentation and a trailing newline, because these files are committed in staged
// releases and read in diffs. A manifest that reformats on every generation makes every release
// diff look like a rewrite.
func marshalManifest(manifest map[string]any) ([]byte, error) {
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("rendering the package manifest: %w", err)
	}
	return append(encoded, '\n'), nil
}
