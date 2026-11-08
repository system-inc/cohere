package load

import (
	"os"
	"path/filepath"
	"reflect"
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

// One go env gives what two gave one after the other: the whole environment, the build target and the go environment
// the inputs record names are those of go env asked for `GOFLAGS`, then asked again under the environment that made,
// whether `GOFLAGS` is the environment's, empty, or the go env file's (#3epjyj2).
func TestOneGoEnvironmentReadIsTheTwo(test *testing.T) {
	test.Parallel()
	directory := test.TempDir()
	environmentFile := filepath.Join(directory, "go.env")
	if writeError := os.WriteFile(
		environmentFile,
		[]byte("GOFLAGS=-tags=fromfile -trimpath\n"),
		0o644,
	); writeError != nil {
		test.Fatal(writeError)
	}
	cases := map[string][]string{
		"Given":        {"GOFLAGS=-buildvcs=false -tags=special  -p=4"},
		"Empty":        {"GOFLAGS="},
		"FromEnvFile":  {"GOFLAGS=", "GOENV=" + environmentFile},
		"ProfileAsked": {"GOFLAGS=-pgo=auto"},
		"WorkspaceOff": {"GOWORK=off"},
	}
	for name, given := range cases {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			twoFull, twoError := withGoFlags(directory, given, func(flags []string) []string {
				return append(flags, "-pgo=off")
			})
			if twoError != nil {
				test.Fatal(twoError)
			}
			twoContext, twoReported, twoContextError := constraintContext(directory, twoFull, nil)
			if twoContextError != nil {
				test.Fatal(twoContextError)
			}
			oneFull, oneContext, oneReported, oneError := sourceContext(directory, given, nil)
			if oneError != nil {
				test.Fatal(oneError)
			}
			if !slices.Equal(oneFull, twoFull) {
				test.Errorf("the environment is %q, two reads made %q", goFlagsOf(oneFull), goFlagsOf(twoFull))
			}
			if !reflect.DeepEqual(oneReported, twoReported) {
				test.Errorf("the go environment is %+v, two reads read %+v", oneReported, twoReported)
			}
			if oneContext.GOOS != twoContext.GOOS ||
				oneContext.GOARCH != twoContext.GOARCH ||
				oneContext.CgoEnabled != twoContext.CgoEnabled ||
				!slices.Equal(oneContext.BuildTags, twoContext.BuildTags) {
				test.Errorf("the build target is %s/%s cgo %t tags %q, two reads read %s/%s cgo %t tags %q",
					oneContext.GOOS, oneContext.GOARCH, oneContext.CgoEnabled, oneContext.BuildTags,
					twoContext.GOOS, twoContext.GOARCH, twoContext.CgoEnabled, twoContext.BuildTags)
			}
			if len(oneReported.goFlags) == 0 || oneReported.goFlags[len(oneReported.goFlags)-1] != "-pgo=off" {
				test.Errorf("the flags %q do not end in -pgo=off", oneReported.goFlags)
			}
		})
	}
}

// goFlagsOf is the `GOFLAGS` entries of an environment.
func goFlagsOf(environment []string) []string {
	var goFlags []string
	for _, variable := range environment {
		if strings.HasPrefix(variable, "GOFLAGS=") {
			goFlags = append(goFlags, variable)
		}
	}
	return goFlags
}
