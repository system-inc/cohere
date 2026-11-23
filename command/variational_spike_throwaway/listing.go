package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
)

// listedPackage is what `go list -json` says of one package in one build, the fields the spike reads.
type listedPackage struct {
	ImportPath      string
	Name            string
	Dir             string
	Standard        bool
	DepOnly         bool
	Export          string
	GoFiles         []string
	CgoFiles        []string
	CompiledGoFiles []string
	Imports         []string
	ImportMap       map[string]string
	Module          *struct {
		Path      string
		GoVersion string
	}
	Error *struct {
		Err string
	}
}

// listedBuild is one build's whole graph as go list gives it, by import path, and the paths the patterns matched.
type listedBuild struct {
	configuration configuration
	cgoEnabled    bool
	packages      map[string]*listedPackage
	matched       []string
}

// noFilesError is go list's error for a package whose every file a build's constraints exclude, which is a package
// absent from that build rather than a broken one.
const noFilesError = "build constraints exclude all Go files"

// listBuild runs `go list -e -deps -export -compiled -json` over patterns in root under build, reading the workspace
// workFile names. -export compiles every package into the build cache, so each has its export data on disk.
func listBuild(root string, patterns []string, build configuration, workFile string) (*listedBuild, error) {
	environment := build.environment(workFile)
	cgoOn, cgoError := cgoEnabled(root, environment)
	if cgoError != nil {
		return nil, cgoError
	}
	arguments := []string{"list", "-e", "-deps", "-export", "-compiled", "-json", "-buildvcs=false"}
	if len(build.tags) != 0 {
		arguments = append(arguments, "-tags="+strings.Join(build.tags, ","))
	}
	arguments = append(arguments, patterns...)
	command := exec.Command("go", arguments...)
	command.Dir, command.Env = root, environment
	var standardError bytes.Buffer
	command.Stderr = &standardError
	output, runError := command.Output()
	if runError != nil {
		return nil, fmt.Errorf("go list for %s: %w: %s", build.target(cgoOn), runError, standardError.String())
	}
	listed := &listedBuild{configuration: build, cgoEnabled: cgoOn, packages: map[string]*listedPackage{}}
	decoder := json.NewDecoder(bytes.NewReader(output))
	for {
		each := &listedPackage{}
		if decodeError := decoder.Decode(each); errors.Is(decodeError, io.EOF) {
			break
		} else if decodeError != nil {
			return nil, fmt.Errorf("reading go list for %s: %w", build.target(cgoOn), decodeError)
		}
		// go list names a package's own files from its directory, and cgo's output by its absolute place in the cache;
		// every file is named absolutely here, as go/packages names them for the oracle.
		for index, file := range each.CompiledGoFiles {
			if !filepath.IsAbs(file) {
				each.CompiledGoFiles[index] = filepath.Join(each.Dir, file)
			}
		}
		listed.packages[each.ImportPath] = each
		if !each.DepOnly {
			listed.matched = append(listed.matched, each.ImportPath)
		}
	}
	return listed, nil
}

// exports is the build's export data files by import path, as the merging importer reads them.
func (listed *listedBuild) exports() map[string]string {
	files := make(map[string]string, len(listed.packages))
	for path, each := range listed.packages {
		if each.Export != "" {
			files[path] = each.Export
		}
	}
	return files
}

// sameArchitecture refuses builds that don't share one architecture: the variational check is given one Sizes, and a
// word size that differs by build is a fork the spike's model doesn't hold.
func sameArchitecture(builds []configuration) (string, error) {
	architecture := builds[0].architecture
	for _, build := range builds[1:] {
		if build.architecture != architecture {
			return "", fmt.Errorf("builds mix architectures, %s and %s: the variational check takes one Sizes, so "+
				"every build must share one architecture", architecture, build.architecture)
		}
	}
	return architecture, nil
}
