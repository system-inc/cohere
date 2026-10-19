package tailwind

import (
	"bufio"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// signatureOracles are Tailwind's own signatures, by hash, for every class arbitraryUtilities searches
// and for the classes a repository writes, on three themes.
//
// Regenerate from the repository root with:
//
//	T=internal/lint/rules/tailwind/tools/generate_signatures
//	D=internal/lint/rules/tailwind/collapse/testdata
//	node $T/signatures.mjs $D/public_theme/theme.css --resolve-root ahra: --targets $D/canonical_singles_public.json > $D/signatures_public.txt.gz
//	node $T/signatures.mjs ahra:app/_theme/styles/theme.css --targets $D/canonical_singles.json > $D/signatures.txt.gz
//	node $T/signatures.mjs connected:app/_theme/styles/theme.css > $D/signatures_connected.txt.gz
var signatureOracles = []struct {
	name string
	file string
}{
	{name: "public", file: "signatures_public.txt.gz"},
	{name: "ahra", file: "signatures.txt.gz"},
	{name: "connected", file: "signatures_connected.txt.gz"},
}

// signatureOracleEntry is one class's line: its signature's hash under each feature set and the
// properties the default signature declares.
type signatureOracleEntry struct {
	className   string
	collapsed   string
	uncollapsed string
	properties  string
	isTarget    bool
}

// TestSignatureAgreesWithTailwind holds every signature this port calls exact to upstream's, under the
// plugin's default options and with collapse off, and reports how many classes it leaves unknown.
func TestSignatureAgreesWithTailwind(t *testing.T) {
	t.Parallel()
	for _, oracle := range signatureOracles {
		t.Run(oracle.name, func(t *testing.T) {
			t.Parallel()
			entryPoint, entries := readSignatureOracle(t, oracle.file)
			system := oracleSystem(t, entryPoint)
			compiler := NewSignatureCompiler(system)

			collapsedFeatures := SignatureFeaturesFor(true, true)
			uncollapsedFeatures := SignatureFeaturesFor(false, true)
			var disagreements, outOfBound []string
			unknownByRoot := map[string]int{}
			disagreeByRoot := map[string]int{}
			outOfBoundByRoot := map[string]int{}
			exact, unknown := 0, 0
			for _, entry := range entries {
				// A written class with variants or importance is canonicalized by its bare utility, so its
				// own signature is never asked for.
				if hasVariantsOrImportance(entry.className, system) {
					continue
				}
				for _, side := range []struct {
					features SignatureFeatures
					expected string
					name     string
				}{
					{collapsedFeatures, entry.collapsed, "collapsed"},
					{uncollapsedFeatures, entry.uncollapsed, "uncollapsed"},
				} {
					signature, state := compiler.ClassSignature(entry.className, side.features)
					switch state {
					case SignatureUnknown:
						if side.name == "collapsed" {
							unknown++
							unknownByRoot[signatureRootOf(entry.className)]++
							if entry.properties != "-" {
								properties := map[string]bool{}
								for _, property := range strings.Fields(entry.properties) {
									properties[property] = true
								}
								if bound := compiler.ClassSignatureBound(entry.className, side.features); !bound.Admits(properties) {
									outOfBoundByRoot[signatureRootOf(entry.className)]++
									outOfBound = append(outOfBound, entry.className+": upstream declares ["+entry.properties+"], bound "+describeBound(bound))
								}
							}
						}
					case SignatureNone:
						if side.expected != "-" {
							disagreements = append(disagreements, entry.className+" ("+side.name+"): compiles here to nothing, upstream to a signature")
						}
					case SignatureExact:
						if side.name == "collapsed" {
							exact++
						}
						if got := signatureHash(signature); got != side.expected {
							if side.name == "collapsed" {
								disagreeByRoot[signatureRootOf(entry.className)]++
							}
							disagreements = append(disagreements, entry.className+" ("+side.name+"): "+strings.ReplaceAll(signature, "\n", " "))
						}
					}
				}
			}
			sort.Strings(disagreements)
			if len(disagreements) > 0 {
				shown := firstPerRoot(disagreements)
				if len(shown) > 40 {
					shown = shown[:40]
				}
				t.Errorf("%d signatures called exact disagree with upstream's, starting with:\n  %s", len(disagreements), strings.Join(shown, "\n  "))
			}
			sort.Strings(outOfBound)
			if len(outOfBound) > 0 {
				shown := firstPerRoot(outOfBound)
				if len(shown) > 40 {
					shown = shown[:40]
				}
				t.Errorf("%d unknown signatures fall outside their bound, so the bail would miss them, starting with:\n  %s", len(outOfBound), strings.Join(shown, "\n  "))
			}
			t.Logf("%d classes: %d exact, %d unknown; unknown by root: %s", len(entries), exact, unknown, topCounts(unknownByRoot, 25))
			t.Logf("disagreements by root: %s", topCounts(disagreeByRoot, 200))
			t.Logf("out of bound by root: %s", topCounts(outOfBoundByRoot, 200))
		})
	}
}

// firstPerRoot keeps the first message for each class root, so a report shows every kind of failure
// rather than forty of the first.
func firstPerRoot(messages []string) []string {
	seen := map[string]bool{}
	var kept []string
	for _, message := range messages {
		className, _, _ := strings.Cut(message, " ")
		root := signatureRootOf(strings.TrimSuffix(className, ":"))
		if !seen[root] {
			seen[root] = true
			kept = append(kept, message)
		}
	}
	return kept
}

// hasVariantsOrImportance reports whether any reading of a class carries a variant or `!`.
func hasVariantsOrImportance(className string, system *LoadedDesignSystem) bool {
	for _, candidate := range ParseCandidate(prefixedClass(className, system), system) {
		if len(candidate.Variants) > 0 || candidate.Important {
			return true
		}
	}
	return false
}

// describeBound renders a bound for a failure message.
func describeBound(bound SignatureBound) string {
	if bound.Unbounded {
		return "unbounded"
	}
	render := func(set map[string]bool) string {
		names := make([]string, 0, len(set))
		for name := range set {
			names = append(names, name)
		}
		sort.Strings(names)
		return strings.Join(names, " ")
	}
	return "certain [" + render(bound.Certain) + "] maybe [" + render(bound.Maybe) + "]"
}

// signatureHash is the oracle's hash: the first twelve hex digits of the signature's SHA-256.
func signatureHash(signature string) string {
	sum := sha256.Sum256([]byte(signature))
	return hex.EncodeToString(sum[:])[:12]
}

// signatureRootOf is the part of a class before its last dash-separated value, for grouping a report.
func signatureRootOf(className string) string {
	className = strings.TrimPrefix(className, "-")
	if index := strings.IndexAny(className, "/["); index > 0 {
		className = className[:index]
	}
	if index := strings.LastIndex(className, "-"); index > 0 {
		return className[:index]
	}
	return className
}

// topCounts renders the largest counts of a map, largest first.
func topCounts(counts map[string]int, limit int) string {
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(left, right int) bool {
		if counts[keys[left]] != counts[keys[right]] {
			return counts[keys[left]] > counts[keys[right]]
		}
		return keys[left] < keys[right]
	})
	if len(keys) > limit {
		keys = keys[:limit]
	}
	parts := make([]string, len(keys))
	for index, key := range keys {
		parts[index] = key + " " + itoa(counts[key])
	}
	return strings.Join(parts, ", ")
}

// readSignatureOracle reads one capture: the entry point its header names and every class line.
func readSignatureOracle(t *testing.T, name string) (string, []signatureOracleEntry) {
	t.Helper()
	file, err := os.Open(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("open %s: %v", name, err)
	}
	defer file.Close()
	reader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatalf("%s is not gzip: %v", name, err)
	}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	entryPoint := ""
	isTarget := false
	var entries []signatureOracleEntry
	for scanner.Scan() {
		line := scanner.Text()
		if header, isHeader := strings.CutPrefix(line, "# "); isHeader {
			if _, after, found := strings.Cut(header, " entry "); found {
				entryPoint = after
			}
			if header == "targets" {
				isTarget = true
			}
			continue
		}
		fields := strings.SplitN(line, "\t", 4)
		if len(fields) != 4 {
			t.Fatalf("%s: malformed line %q", name, line)
		}
		entries = append(entries, signatureOracleEntry{
			className:   fields[0],
			collapsed:   fields[1],
			uncollapsed: fields[2],
			properties:  fields[3],
			isTarget:    isTarget,
		})
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	if len(entries) == 0 {
		t.Fatalf("%s holds no classes", name)
	}
	return entryPoint, entries
}
