package markdown

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/format/differential"
	"github.com/system-inc/cohere/internal/format/doc"
	"github.com/system-inc/cohere/internal/format/markdown/mdast"
	"github.com/system-inc/cohere/internal/format/prettier"
	"github.com/system-inc/cohere/internal/format/printing"
	release "github.com/system-inc/cohere/internal/release/packaging"
)

// The printer's acceptance test, run from this package: the differential's comparison over every .md
// file in the corpora, the port as the candidate and the embedded Prettier as the oracle. It shares
// TestCorpora's oracle cache, so the two never format a file twice, and it builds without the native
// package, whose other printers are mid-port.
//
//	COHERE_MARKDOWN_CORPORA   colon-separated repository roots
//	COHERE_MARKDOWN_REPORT    optional file to write every difference to
//	COHERE_FORMAT_CACHE       oracle cache directory (default: the user cache directory)

// markdownCandidate formats .md files with the port and refuses the rest.
type markdownCandidate struct {
	options   prettier.Options
	textToDoc printing.TextToDoc
}

// oracleTextToDoc formats embedded code with the embedded Prettier, standing in for native printers
// that do not exist yet, so the embed plumbing (fences, front matter) is measured on its own. Set
// COHERE_MARKDOWN_EMBED_ORACLE to use it.
func oracleTextToDoc(t *testing.T, options prettier.Options) printing.TextToDoc {
	if os.Getenv("COHERE_MARKDOWN_EMBED_ORACLE") == "" {
		return nil
	}
	engine, err := prettier.New(options)
	if err != nil {
		t.Fatal(err)
	}
	fileNames := map[string]string{
		"typescript": "embedded.tsx", "babel": "embedded.js", "json": "embedded.json", "json5": "embedded.json5",
		"jsonc": "embedded.jsonc", "yaml": "embedded.yaml", "graphql": "embedded.graphql", "css": "embedded.css",
		"scss": "embedded.scss", "less": "embedded.less", "markdown": "embedded.md",
	}
	return func(text string, parser string) (doc.Doc, error) {
		fileName := parser
		if !strings.Contains(parser, ".") {
			fileName = fileNames[parser]
		}
		formatted, err := engine.Format(fileName, text)
		if err != nil {
			return nil, err
		}
		return doc.Text(strings.TrimSuffix(formatted, "\n")), nil
	}
}

func (candidate markdownCandidate) Handles(fileName string) bool {
	return strings.EqualFold(filepath.Ext(fileName), ".md")
}

func (candidate markdownCandidate) Format(fileName string, text string) (string, error) {
	if !candidate.Handles(fileName) {
		return "", fmt.Errorf("markdown: not a .md file: %s", fileName)
	}
	return Format(text, candidate.options, candidate.textToDoc)
}

// dependsOnEmbed reports whether Prettier formats part of the file as another language: a fenced code
// block whose language infers a parser, or yaml or toml front matter with content. Those parts cannot
// match until that language's printer exists, so the report separates them.
func dependsOnEmbed(text string) bool {
	text = strings.ReplaceAll(strings.ReplaceAll(strings.TrimPrefix(text, "\ufeff"), "\r\n", "\n"), "\r", "\n")
	ast, err := mdast.ParseMarkdown(text)
	if err != nil {
		return false
	}
	found := false
	var walk func(node *Node)
	walk = func(node *Node) {
		switch node.NodeType {
		case "code":
			if node.Lang != nil && (*node.Lang == "angular-html" || inferParserForLanguage(*node.Lang) != "" ||
				(*node.Lang == "angular-ts")) {
				found = true
			}
		case "frontMatter":
			if supportedEmbedFrontMatterLanguages[node.FrontMatter.Language] &&
				strings.Trim(node.FrontMatter.Value, javaScriptSpaceText) != "" {
				found = true
			}
		}
		for _, child := range node.Children {
			walk(child)
		}
	}
	walk(ast)
	return found
}

func TestCorpusFormatMatchesOracle(t *testing.T) {
	roots := os.Getenv("COHERE_MARKDOWN_CORPORA")
	if roots == "" {
		t.Skip("set COHERE_MARKDOWN_CORPORA to measure; this is a measuring run, not a unit test")
	}

	bundles, err := prettier.Bundles()
	if err != nil {
		t.Fatal(err)
	}
	digest, err := release.DigestBundleFiles(bundles.Files)
	if err != nil {
		t.Fatal(err)
	}
	directory := os.Getenv("COHERE_FORMAT_CACHE")
	if directory == "" {
		base, err := os.UserCacheDir()
		if err != nil {
			t.Fatal(err)
		}
		directory = filepath.Join(base, "cohere", "prettier-oracle")
	}
	enumerator, err := prettier.New(prettier.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}

	var report strings.Builder
	for _, root := range strings.Split(roots, ":") {
		root = strings.TrimSpace(root)
		if strings.HasPrefix(root, "~/") {
			home, _ := os.UserHomeDir()
			root = filepath.Join(home, root[2:])
		}
		if root == "" {
			continue
		}

		structureIgnore := filepath.Join(root, "libraries", "structure", "code-quality", "PrettierIgnoreDefaults.ts")
		enumeration, err := enumerator.Enumerate(root, structureIgnore)
		if err != nil {
			t.Fatalf("enumerating %s: %v", root, err)
		}
		var files []string
		for _, file := range enumeration.Files {
			if strings.EqualFold(filepath.Ext(file), ".md") {
				files = append(files, file)
			}
		}

		resolution, err := prettier.ResolveOptions(root)
		if err != nil {
			t.Fatalf("resolving the Prettier config for %s: %v", root, err)
		}
		options := resolution.Options
		cache := &differential.OracleCache{
			Directory: directory,
			// TestCorpora's identity, character for character, so both share one cache.
			Identity: digest + "|" + string(bundles.Origin) + "|filepath|" + fmt.Sprintf("%+v", options),
		}
		newOracle := func() (differential.Formatter, error) { return prettier.New(options) }
		newCandidate := func() (differential.Formatter, error) {
			return markdownCandidate{options: options, textToDoc: oracleTextToDoc(t, options)}, nil
		}

		compared, err := differential.Compare(root, files, newOracle, newCandidate, runtime.NumCPU(), cache)
		if err != nil {
			t.Fatalf("comparing %s: %v", root, err)
		}

		type count struct{ identical, measured, rewriteIdentical, rewriteTotal int }
		var all, withoutEmbed count
		differences := map[string]int{}
		var different []differential.FileResult
		for _, result := range compared.Files {
			if result.Outcome == differential.OracleFailed {
				continue
			}
			path := result.Path
			if !filepath.IsAbs(path) {
				path = filepath.Join(root, path)
			}
			source, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			embedded := dependsOnEmbed(string(source))
			for _, tally := range []*count{&all, &withoutEmbed} {
				if tally == &withoutEmbed && embedded {
					continue
				}
				tally.measured++
				if result.Outcome == differential.Identical {
					tally.identical++
				}
				if result.OracleRewrites {
					tally.rewriteTotal++
					if result.Outcome == differential.Identical {
						tally.rewriteIdentical++
					}
				}
			}
			if result.Outcome != differential.Identical {
				different = append(different, result)
				if !embedded {
					differences[string(result.Outcome)]++
				}
			}
		}

		percent := func(part, whole int) float64 {
			if whole == 0 {
				return 0
			}
			return 100 * float64(part) / float64(whole)
		}
		t.Logf("%s: md %d/%d identical (%.2f%%), rewrite subset %d/%d (%.2f%%)",
			root, all.identical, all.measured, percent(all.identical, all.measured),
			all.rewriteIdentical, all.rewriteTotal, percent(all.rewriteIdentical, all.rewriteTotal))
		t.Logf("%s without embedded-language files: md %d/%d identical (%.2f%%), rewrite subset %d/%d (%.2f%%); outcomes %v",
			root, withoutEmbed.identical, withoutEmbed.measured, percent(withoutEmbed.identical, withoutEmbed.measured),
			withoutEmbed.rewriteIdentical, withoutEmbed.rewriteTotal, percent(withoutEmbed.rewriteIdentical, withoutEmbed.rewriteTotal),
			differences)

		sort.Slice(different, func(left, right int) bool { return different[left].Path < different[right].Path })
		for index, result := range different {
			line := fmt.Sprintf("%s: %s: %s\n", result.Outcome, result.Path, result.Detail)
			report.WriteString(line)
			if index < 15 {
				t.Log(strings.TrimSpace(line))
			}
		}
	}

	if path := os.Getenv("COHERE_MARKDOWN_REPORT"); path != "" {
		if err := os.WriteFile(path, []byte(report.String()), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
