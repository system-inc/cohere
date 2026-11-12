package load

import (
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// The load from source's `GOFLAGS` is the one go reads, the environment's or the go env file's, every flag kept
// in order, -trimpath among them, with -pgo=off last, a flag a runner of prebuilt tests lets through: without it
// go list prints a second variant of each package a command with a default.pgo profile builds.
func TestTheWholeEnvironmentKeepsGoFlagsAndTurnsProfilesOff(test *testing.T) {
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

			goFlags := goFlagsOf(goEnvironmentOf(test, directory, []string{given}).fullEnvironment)
			if !slices.Equal(goFlags, []string{want}) {
				test.Fatalf("want only %q, got %q", want, goFlags)
			}
		})
	}
}

// One go env, asked under the environment as given, answers what go env asked under the whole environment a load runs
// with answers, and what it asked under go list -compiled's: every variable a reader reads is the same under all
// three, but `GOFLAGS`, which value reports as the whole environment's own. go env defines no flag of `GOFLAGS` but
// -n and -x, and skips the rest (cmd/go's envcmd and base.SetFromGOFLAGS), so neither the -pgo=off a load adds nor
// the -trimpath go list -compiled takes out moves anything else it reports, whether `GOFLAGS` is the environment's,
// empty, or the go env file's, -trimpath in each but the empty one. The build target and the go environment the inputs
// record names are those the whole environment's own go env reads (#3epjyj2, #4rpt1jh).
func TestGoEnvironmentUnderTheWholeEnvironmentIsTheGivenOnes(test *testing.T) {
	test.Parallel()
	directory := test.TempDir()
	writeListingFiles(test, directory, map[string]string{
		"go.mod":   "module example.com/asked\n\ngo 1.27\n",
		"go.env":   "GOFLAGS=-tags=fromfile -trimpath\n",
		"asked.go": "package asked\n",
	})
	environmentFile := filepath.Join(directory, "go.env")
	cases := map[string][]string{
		"Given":        {"GOFLAGS=-buildvcs=false -tags=special  -trimpath -p=4"},
		"Empty":        {"GOFLAGS=", "GOENV=off"},
		"FromEnvFile":  {"GOFLAGS=", "GOENV=" + environmentFile},
		"ProfileAsked": {"GOFLAGS=-pgo=auto --trimpath=true"},
		"WorkspaceOff": {"GOWORK=off", "GOFLAGS=-trimpath"},
	}
	for name, given := range cases {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			goEnvironment := goEnvironmentOf(test, directory, given)
			if len(goEnvironment.answer) != len(goEnvironmentNames) {
				test.Fatalf("go env answered %d variables of the %d asked", len(goEnvironment.answer),
					len(goEnvironmentNames))
			}
			underWhole, wholeError := askGoEnvironment(goEnvironment.workspace, goEnvironment.fullEnvironment)
			if wholeError != nil {
				test.Fatal(wholeError)
			}
			underTranslated, translatedError := askGoEnvironment(goEnvironment.workspace,
				translatedEnvironment(goEnvironment))
			if translatedError != nil {
				test.Fatal(translatedError)
			}
			for _, variable := range goEnvironmentNames {
				if read := goEnvironment.value(variable); read != underWhole[variable] {
					test.Errorf("%s reads %q, go env under the whole environment reports %q", variable, read,
						underWhole[variable])
				}
				if variable == "GOFLAGS" {
					continue
				}
				if given := goEnvironment.answer[variable]; given != underTranslated[variable] {
					test.Errorf("%s is %q under the environment as given, %q under go list -compiled's", variable,
						given, underTranslated[variable])
				}
			}
			// Every case but the empty one holds -trimpath, the environment's or the go env file's.
			if name != "Empty" && !strings.Contains(goEnvironment.answer["GOFLAGS"], "trimpath") {
				test.Errorf("the given flags %q hold no -trimpath, so the case proves nothing of it",
					goEnvironment.answer["GOFLAGS"])
			}
			wholeContext, wholeReported, wholeContextError := constraintContext(goEnvironment.workspace, underWhole,
				nil)
			if wholeContextError != nil {
				test.Fatal(wholeContextError)
			}
			if !reflect.DeepEqual(goEnvironment.reported, wholeReported) {
				test.Errorf("the go environment is %+v, the whole environment's go env read %+v",
					goEnvironment.reported, wholeReported)
			}
			if goEnvironment.buildContext.GOOS != wholeContext.GOOS ||
				goEnvironment.buildContext.GOARCH != wholeContext.GOARCH ||
				goEnvironment.buildContext.CgoEnabled != wholeContext.CgoEnabled ||
				!slices.Equal(goEnvironment.buildContext.BuildTags, wholeContext.BuildTags) {
				test.Errorf("the build target is %s/%s cgo %t tags %q, the whole environment's go env read %s/%s "+
					"cgo %t tags %q", goEnvironment.buildContext.GOOS, goEnvironment.buildContext.GOARCH,
					goEnvironment.buildContext.CgoEnabled, goEnvironment.buildContext.BuildTags, wholeContext.GOOS,
					wholeContext.GOARCH, wholeContext.CgoEnabled, wholeContext.BuildTags)
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
