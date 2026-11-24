package go_engine

import "testing"

// The key a toolchain's ports are kept under moves with the toolchain and with the environment, and never with where
// it is asked (#2htryg8): two test runs share one listing only under the same `GOROOT` and `GOVERSION` and the same
// variables, so a run under another toolchain never reads the first one's ports, and a run in another directory, whose
// `PWD` alone differs, reads them without a child.
func TestThePortsKeyMovesWithTheToolchainAndTheEnvironmentAndNeverWithTheAsker(test *testing.T) {
	test.Parallel()

	toolchain := map[string]string{"GOROOT": "/toolchains/first", "GOVERSION": "go1.27.4", "GOOS": "darwin"}
	environment := []string{"PATH=/usr/bin", "PWD=/projects/first", "GOENV=off"}
	plain := portsKey(toolchain, environment)
	for name, other := range map[string]string{
		"another GOROOT": portsKey(map[string]string{"GOROOT": "/toolchains/second", "GOVERSION": "go1.27.4",
			"GOOS": "darwin"}, environment),
		"another GOVERSION": portsKey(map[string]string{"GOROOT": "/toolchains/first", "GOVERSION": "go1.27.5",
			"GOOS": "darwin"}, environment),
		"another variable": portsKey(toolchain, []string{"PATH=/usr/bin", "PWD=/projects/first", "GOENV=off",
			"GOTOOLCHAIN=local"}),
	} {
		if other == plain {
			test.Errorf("under %s the ports read the plain key", name)
		}
	}
	if moved := portsKey(toolchain, []string{"PATH=/usr/bin", "PWD=/projects/second", "GOENV=off"}); moved != plain {
		test.Error("asked from another directory, its `PWD` alone differing, the ports read another key")
	}
}
