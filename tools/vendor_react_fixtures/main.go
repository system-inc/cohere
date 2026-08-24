// Command vendor_react_fixtures re-pulls React's error-named compiler fixtures at a chosen commit.
//
// The corpus under `internal/reactconformance/testdata/fixtures` is vendored rather than fetched at
// test time, so this tool is what makes the pin reproducible: it is how the vendored tree was
// produced and how it gets moved to a new sha. It writes nothing unless every count it expects
// holds, because the whole value of a vendored corpus is that a human can review the diff, and a
// diff produced by a partial fetch is a review of the wrong thing.
//
// # The counting trap this tool is built around
//
// The research pass this corpus comes from reported 221 error fixtures. The real number is 325. The
// cause was a grep anchored at the fixture root, which excluded the nine subdirectories that hold
// 103 of them — wrong by a third, and it read exactly like a clean result. It was caught only
// because a parallel investigation returned a different number.
//
// So this tool does not filter by path prefix at any point. It walks the full tree, keys fixtures
// by their path relative to the fixture root, and reports the top-level and nested split separately
// so the number that moved last time is visible rather than summed away.
//
// # Why the git-tree API and not the contents API
//
// The GitHub contents API caps at 1,000 entries and silently repeats pages rather than erroring,
// which produced a plausible-but-wrong file count during the research pass. The git-tree API with
// `recursive=1` reports `truncated`, and this tool refuses to proceed when it is true. That check
// is the difference between a measurement and a guess.
//
//	go run ./tools/vendor_react_fixtures -sha <commit> -out internal/reactconformance/testdata/fixtures
//
// Pass -dry-run to report the counts without writing.
package main

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// upstreamFixtureDirectory is the fixture root inside facebook/react.
const upstreamFixtureDirectory = "compiler/packages/babel-plugin-react-compiler/src/__tests__/fixtures/compiler"

// expectedFixtureCount is what a correct run finds at the pinned sha.
//
// Asserted rather than reported, because a vendoring tool that writes whatever it found cannot tell
// a corpus that shrank from a fetch that failed halfway. When bumping the pin, expect this to need
// updating and read the diff before changing it.
const expectedFixtureCount = 325

func main() {
	sha := flag.String("sha", "", "facebook/react commit to vendor from (required)")
	out := flag.String("out", "", "directory to write the fixture pairs into (required unless -dry-run)")
	dryRun := flag.Bool("dry-run", false, "report the counts without writing")
	expect := flag.Int("expect", expectedFixtureCount, "fixture pairs a correct run finds; a mismatch aborts")
	flag.Parse()

	if *sha == "" {
		fail("-sha is required; vendoring from a moving branch is the thing this corpus exists not to do")
	}
	if *out == "" && !*dryRun {
		fail("-out is required unless -dry-run")
	}

	names, err := errorFixtureNames(*sha)
	if err != nil {
		fail("listing the tree: %v", err)
	}

	topLevel, nested := 0, 0
	subdirectories := map[string]int{}
	for _, name := range names {
		if !strings.Contains(name, "/") {
			topLevel++
			continue
		}
		nested++
		subdirectories[path.Dir(name)]++
	}

	fmt.Printf("sha              %s\n", *sha)
	fmt.Printf("error fixtures   %d  (%d top-level, %d across %d subdirectories)\n",
		len(names), topLevel, nested, len(subdirectories))
	for _, directory := range sortedKeys(subdirectories) {
		fmt.Printf("                   %-40s %d\n", directory, subdirectories[directory])
	}

	// A run that found nothing nested is the exact shape of the depth-anchored defect, and it would
	// otherwise write a corpus two thirds the size and report success.
	if nested == 0 {
		fail("found no fixtures in subdirectories at all, which is the signature of a path-anchored filter rather than an empty tree")
	}
	if len(names) != *expect {
		fail("found %d error fixtures, expected %d; if upstream really changed, read the diff and pass -expect", len(names), *expect)
	}

	if *dryRun {
		fmt.Println("dry run, nothing written")
		return
	}

	written, err := writeFixtures(*sha, names, *out)
	if err != nil {
		fail("writing fixtures: %v", err)
	}
	fmt.Printf("wrote            %d files (%d pairs) into %s\n", written, written/2, *out)
}

// errorFixtureNames returns the error-named inputs, relative to the fixture root.
func errorFixtureNames(sha string) ([]string, error) {
	url := fmt.Sprintf("https://api.github.com/repos/facebook/react/git/trees/%s?recursive=1", sha)
	body, err := get(url)
	if err != nil {
		return nil, err
	}
	defer body.Close()

	var tree struct {
		Truncated bool `json:"truncated"`
		Tree      []struct {
			Path string `json:"path"`
			Type string `json:"type"`
		} `json:"tree"`
	}
	if err := json.NewDecoder(body).Decode(&tree); err != nil {
		return nil, err
	}

	// The contents API silently repeats pages when it caps; the tree API says so instead. A
	// truncated tree is missing files with no way to know which, so this refuses rather than
	// vendoring a corpus of unknown completeness.
	if tree.Truncated {
		return nil, fmt.Errorf("the git tree came back truncated, so the listing is incomplete and any count from it is wrong")
	}

	var names []string
	for _, entry := range tree.Tree {
		if entry.Type != "blob" {
			continue
		}
		name, found := strings.CutPrefix(entry.Path, upstreamFixtureDirectory+"/")
		if !found || strings.HasSuffix(name, ".expect.md") {
			continue
		}
		base := path.Base(name)
		if strings.HasPrefix(base, "error.") || strings.HasPrefix(base, "todo.error.") {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names, nil
}

// writeFixtures pulls the tarball once and writes each pair.
//
// One tarball rather than 650 blob requests: it is a single round trip, it cannot be rate-limited
// halfway into a corpus, and it gives byte-identical content to what a clone would.
func writeFixtures(sha string, names []string, out string) (int, error) {
	wanted := map[string]bool{}
	for _, name := range names {
		wanted[name] = true
		wanted[strings.TrimSuffix(name, filepath.Ext(name))+".expect.md"] = true
	}

	body, err := get(fmt.Sprintf("https://api.github.com/repos/facebook/react/tarball/%s", sha))
	if err != nil {
		return 0, err
	}
	defer body.Close()

	gzipReader, err := gzip.NewReader(body)
	if err != nil {
		return 0, err
	}
	defer gzipReader.Close()

	found := map[string]bool{}
	written := 0
	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return written, err
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}

		// The tarball root is a generated `owner-repo-sha` directory, so the first path segment is
		// dropped rather than matched.
		_, rest, found1 := strings.Cut(header.Name, "/")
		if !found1 {
			continue
		}
		name, found2 := strings.CutPrefix(rest, upstreamFixtureDirectory+"/")
		if !found2 || !wanted[name] {
			continue
		}

		destination := filepath.Join(out, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return written, err
		}
		contents, err := io.ReadAll(tarReader)
		if err != nil {
			return written, err
		}
		if err := os.WriteFile(destination, contents, 0o644); err != nil {
			return written, err
		}
		found[name] = true
		written++
	}

	// Every input must have arrived with its expectation. Upstream pairs them 1:1 with zero missing
	// at the pinned sha, so a gap here means the fetch dropped something, and a corpus quietly
	// short a few pairs still scores and still looks complete.
	var missing []string
	for name := range wanted {
		if !found[name] {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return written, fmt.Errorf("%d files listed in the tree never appeared in the tarball, first few: %v",
			len(missing), missing[:min(5, len(missing))])
	}
	return written, nil
}

func get(url string) (io.ReadCloser, error) {
	request, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	// Unauthenticated GitHub allows 60 requests an hour, which two runs of this tool can exhaust.
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		return nil, fmt.Errorf("%s: %s", url, response.Status)
	}
	return response.Body, nil
}

func sortedKeys(counts map[string]int) []string {
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func fail(format string, arguments ...any) {
	fmt.Fprintf(os.Stderr, "vendor_react_fixtures: "+format+"\n", arguments...)
	os.Exit(1)
}
