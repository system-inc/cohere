package load

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/project"
)

// targetedModuleFiles are a module whose listing turns on the target: a file for each of three operating systems and
// one for an architecture, each importing a package of its own so a file matched to the wrong target changes the
// imports as well as the files, a file for unix alone, a file only a custom tag builds, and a command, whose walk
// takes the linker's dependencies on the target.
var targetedModuleFiles = map[string]string{
	"go.mod":              "module example.com/targeted\n\ngo 1.27\n",
	"targeted.go":         "package targeted\n\nimport \"os\"\n\n// Name is the program's name.\nfunc Name() string { return os.Args[0] }\n",
	"targeted_windows.go": "package targeted\n\nimport _ \"container/list\"\n",
	"targeted_linux.go":   "package targeted\n\nimport _ \"container/ring\"\n",
	"targeted_freebsd.go": "package targeted\n\nimport _ \"container/heap\"\n",
	"targeted_arm64.go":   "package targeted\n\nimport _ \"unicode/utf16\"\n",
	"unix.go":             "//go:build unix\n\npackage targeted\n\nimport _ \"text/tabwriter\"\n",
	"tagged.go":           "//go:build cohere_listing_tag\n\npackage targeted\n\nimport _ \"hash/crc32\"\n",
	"command/tool/main.go": "package main\n\nimport \"example.com/targeted\"\n\n" +
		"func main() { _ = targeted.Name() }\n",
}

// The in-process listing is go list's on every target the toolchain lists, file for file and import for import,
// a command's linker dependencies among them, and a target whose every command links externally is doubted, so go
// list answers it: the extra builds of `--other-builds` list in this process as the host's does (#37vy8cm).
func TestInProcessListingIsGoListsAnswerOnEveryTarget(test *testing.T) {
	test.Parallel()
	directory := test.TempDir()
	writeListingFiles(test, directory, targetedModuleFiles)
	ports, portsError := project.ReadPorts(directory, []string{"GOWORK=off"})
	if portsError != nil {
		test.Fatal(portsError)
	}
	if len(ports) < 40 {
		test.Fatalf("go tool dist list named %d targets, too few to be the toolchain's", len(ports))
	}
	answered := 0
	for index, port := range ports {
		target := port.OperatingSystem + "/" + port.Architecture
		// The tag rides on every other target, so both sides of the tagged file are listed across the targets.
		goFlags := "GOFLAGS="
		if index%2 == 0 {
			goFlags = "GOFLAGS=-tags=cohere_listing_tag"
		}
		environment := []string{"GOWORK=off", "GOOS=" + port.OperatingSystem, "GOARCH=" + port.Architecture,
			"CGO_ENABLED=0", goFlags}
		if linksExternally(port.OperatingSystem, port.Architecture) {
			test.Run(strings.ReplaceAll(target, "/", "_"), func(test *testing.T) {
				test.Parallel()
				requireExternallyLinked(test, directory, environment, "example.com/targeted/command/tool")
			})
			continue
		}
		answered++
		test.Run(strings.ReplaceAll(target, "/", "_"), func(test *testing.T) {
			test.Parallel()
			requireGoListsAnswer(test, directory, environment, []string{"./..."})
		})
	}
	// Every target but the few that link externally is answered, the extra builds' among them.
	if answered < len(ports)-6 {
		test.Errorf("the lister answered %d of %d targets", answered, len(ports))
	}
}

// The same holds for each extra build `--other-builds` checks at cohere's own root, listed as the engine lists it:
// the builds the engine reads for cohere's census, each under the workspace's environment and the build's own
// (see the engine's extraBuildRun), so no extra build forks go list.
func TestInProcessListingIsGoListsAnswerOnCoheresOtherBuilds(test *testing.T) {
	test.Parallel()
	root, absoluteError := filepath.Abs(filepath.Join("..", "..", ".."))
	if absoluteError != nil {
		test.Fatal(absoluteError)
	}
	workspace, workspaceError := project.ReadWorkspace(root)
	if workspaceError != nil {
		test.Fatal(workspaceError)
	}
	census, censusError := project.TakeCensus(workspace)
	if censusError != nil {
		test.Fatal(censusError)
	}
	ports, portsError := project.ReadPorts(workspace.Root, workspace.Environment())
	if portsError != nil {
		test.Fatal(portsError)
	}
	goEnvironment, environmentError := ReadGoEnvironment(workspace.Root, workspace.Environment(), nil)
	if environmentError != nil {
		test.Fatal(environmentError)
	}
	buildContext, goFlags := goEnvironment.Target()
	// The targets cohere's go block adds, as the engine reads them (settings.parseTargets), matched to their ports.
	contents, readError := os.ReadFile(filepath.Join(workspace.Root, "CohereSettings.json"))
	if readError != nil {
		test.Fatal(readError)
	}
	var settingsFile struct {
		Go struct {
			Targets []string `json:"targets"`
		} `json:"go"`
	}
	if decodeError := json.Unmarshal(contents, &settingsFile); decodeError != nil {
		test.Fatal(decodeError)
	}
	var extraTargets []project.Port
	for _, target := range settingsFile.Go.Targets {
		for _, port := range ports {
			if port.OperatingSystem+"/"+port.Architecture == target {
				extraTargets = append(extraTargets, port)
			}
		}
	}
	builds, _, buildsError := project.BuildsFor(census, project.HostBuild(buildContext, goFlags), ports, extraTargets)
	if buildsError != nil {
		test.Fatal(buildsError)
	}
	if len(builds) < 2 {
		test.Fatalf("cohere's census needs %d builds, so no extra build's listing is compared", len(builds))
	}
	for _, extra := range builds[1:] {
		test.Run(strings.NewReplacer("/", "_", " ", "_", ",", "_").Replace(extra.String()), func(test *testing.T) {
			test.Parallel()
			environment := append(workspace.Environment(), extra.Environment()...)
			requireGoListsAnswer(test, workspace.Root, environment, workspace.Patterns())
		})
	}
}

// requireDoubted fails unless the in-process lister doubts listing directory's ./... under environment, for a reason
// that holds reason.
func requireDoubted(test *testing.T, directory string, extra []string, reason string) {
	test.Helper()
	goEnvironment := goEnvironmentOf(test, directory, extra)
	listed, doubt := listInProcess(goEnvironment, []string{"./..."}, nil, nil, nil, nil)
	if doubt == "" {
		test.Fatalf("the lister answered with %d packages", len(listed.packages))
	}
	if !strings.Contains(doubt, reason) {
		test.Errorf("the lister doubted %q, not for %q", doubt, reason)
	}
}

// requireExternallyLinked fails unless the lister doubts directory's ./... under environment for its external
// linking and go list, answering instead, reports the command as one that needs it: the doubt is the toolchain's own
// rule, never broader.
func requireExternallyLinked(test *testing.T, directory string, extra []string, command string) {
	test.Helper()
	requireDoubted(test, directory, extra, "links externally")
	goEnvironment := goEnvironmentOf(test, directory, extra)
	goList, listError := listWithGoList(goEnvironment.guard, directory, goEnvironment.fullEnvironment,
		[]string{"./..."}, nil)
	if listError != nil {
		test.Fatal(listError)
	}
	listed := goList.byPath[command]
	if listed == nil || listed.Error == nil ||
		!strings.Contains(listed.Error.Message, "requires external (cgo) linking") {
		test.Errorf("go list listed %s as %+v, not as needing external linking", command, listed)
	}
}
