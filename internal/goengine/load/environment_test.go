package load

import (
	"slices"
	"strings"
	"testing"
)

// The load from source's `GOFLAGS` is the one go reads, the environment's or the go env file's, every flag kept
// in order, -trimpath among them, with -pgo=off last, a flag a runner of prebuilt tests lets through: without it
// go list prints a second variant of each package a command with a default.pgo profile builds.
func TestSourceEnvironmentKeepsGoFlagsAndTurnsProfilesOff(test *testing.T) {
	test.Parallel()

	directory := test.TempDir()
	for given, want := range map[string]string{
		"GOFLAGS=-trimpath":                      "GOFLAGS=-trimpath -pgo=off",
		"GOFLAGS=-buildvcs=false -trimpath -p=4": "GOFLAGS=-buildvcs=false -trimpath -p=4 -pgo=off",
		"GOFLAGS=-tags=special":                  "GOFLAGS=-tags=special -pgo=off",
		"GOFLAGS=-pgo=auto":                      "GOFLAGS=-pgo=auto -pgo=off",
	} {
		test.Run(given, func(test *testing.T) {
			test.Parallel()

			environment, environmentError := sourceEnvironment(directory, []string{given})
			if environmentError != nil {
				test.Fatal(environmentError)
			}
			var goFlags []string
			for _, variable := range environment {
				if strings.HasPrefix(variable, "GOFLAGS=") {
					goFlags = append(goFlags, variable)
				}
			}
			if !slices.Equal(goFlags, []string{want}) {
				test.Fatalf("want only %q, got %q", want, goFlags)
			}
		})
	}
}
