package estree

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/system-inc/cohere/internal/corpus"
	"github.com/system-inc/cohere/internal/jsonvalue"
)

// prettierParseScript is the oracle: the fork's own parse (parser "typescript", with the file path, so
// JSX and sourceType are decided the way Prettier decides them for a real file), dumped in the shape
// dumpValue prints. Offsets are UTF-16 there, and the Go side converts to match.
const prettierParseScript = `
import * as prettier from "./src/index.js";
import fs from "node:fs";
const skip = new Set(["loc","range","comments","tokens","parent","start","end","extra"]);
function dump(v) {
  if (v === null || v === undefined) return null;
  if (Array.isArray(v)) return v.map(dump);
  if (typeof v === "bigint" || v instanceof RegExp) return null;
  if (typeof v !== "object") return v;
  if (v.type === undefined) { const o = {}; for (const k of Object.keys(v).sort()) o[k] = dump(v[k]); return o; }
  const o = { type: v.type, range: [v.range[0], v.range[1]] };
  if (v.__contentEnd !== undefined) o.__contentEnd = v.__contentEnd;
  for (const k of Object.keys(v).sort()) { if (skip.has(k) || k === "type" || k.startsWith("__")) continue; o[k] = dump(v[k]); }
  if (o.type === "Literal" && (v.regex || v.bigint)) o.value = null;
  return o;
}
let input = ""; process.stdin.setEncoding("utf8"); process.stdin.on("data", (c) => input += c);
process.stdin.on("end", async () => {
  for (const f of input.split("\n").filter(Boolean)) {
    const text = fs.readFileSync(f, "utf8");
    try {
      const { ast } = await prettier.__debug.parse(text, { parser: "typescript", filepath: f });
      const comments = (ast.comments ?? []).map(c => ({ type: c.type, range: c.range, value: c.value }));
      process.stdout.write(JSON.stringify({ file: f, ast: dump(ast), comments }) + "\n");
    } catch (e) { process.stdout.write(JSON.stringify({ file: f, error: String(e.message).split("\n")[0] }) + "\n"); }
  }
});
`

// TestConvertAgreesWithPrettier is the converter's acceptance test: for every .ts and .tsx file in the
// corpora, the Go tree and comment list must equal what the fork's own parse produces, property for
// property. Off unless COHERE_PRETTIER_FORK names the fork and COHERE_ESTREE_CORPORA lists repository
// roots (colon-separated; each is enumerated with git ls-files, so a nested repository is its own root).
//
// A file the fork cannot parse has no answer to match and is skipped, but counted, and the test fails
// if that count passes a tenth of the corpus: an oracle that rejects everything would otherwise read as
// perfect agreement.
func TestConvertAgreesWithPrettier(test *testing.T) {
	test.Parallel()
	corpora := os.Getenv("COHERE_ESTREE_CORPORA")
	if corpora == "" {
		test.Skip("set COHERE_ESTREE_CORPORA to compare the converter against the fork")
	}
	root := corpus.PrettierFork.Root(test)
	seen := map[string]bool{}
	var files []string
	for _, corpus := range strings.Split(corpora, ":") {
		corpus = strings.TrimSpace(corpus)
		if strings.HasPrefix(corpus, "~/") {
			home, homeError := os.UserHomeDir()
			if homeError != nil {
				test.Fatalf("finding the home directory for %s: %v", corpus, homeError)
			}
			corpus = filepath.Join(home, corpus[2:])
		}
		listing, outputError := exec.Command("git", "-C", corpus, "ls-files", "--", "*.ts", "*.tsx").Output()
		if outputError != nil {
			test.Fatalf("listing %s: %v", corpus, outputError)
		}
		for _, name := range strings.Split(string(listing), "\n") {
			path := filepath.Join(corpus, name)
			if name == "" || seen[path] {
				continue
			}
			if _, statError := os.Stat(path); statError != nil {
				continue
			}
			seen[path] = true
			files = append(files, path)
		}
	}

	command := exec.Command("node", "--input-type=module", "-e", prettierParseScript)
	command.Dir = root
	command.Stdin = strings.NewReader(strings.Join(files, "\n"))
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if runError := command.Run(); runError != nil {
		test.Fatalf("node: %v\n%s", runError, stderr.String())
	}
	oracle := map[string]jsonvalue.JsonObject{}
	for _, line := range strings.Split(stdout.String(), "\n") {
		if line == "" {
			continue
		}
		parsed, parseError := jsonvalue.ParseJsonValue([]byte(line))
		if parseError != nil {
			test.Fatal(parseError)
		}
		record, isObject := parsed.(jsonvalue.JsonObject)
		if !isObject {
			test.Fatalf("the oracle printed a line that is not an object: %s", line)
		}
		file, isString := record["file"].(jsonvalue.JsonString)
		if !isString {
			test.Fatalf("the oracle printed a record with no file name: %s", line)
		}
		oracle[string(file)] = record
	}

	identical, refused, different := 0, 0, 0
	for _, path := range files {
		want, present := oracle[path]
		if !present {
			test.Fatalf("the oracle printed nothing for %s", path)
		}
		if _, failed := want["error"]; failed {
			refused++
			continue
		}
		source, readError := os.ReadFile(path)
		if readError != nil {
			test.Fatal(readError)
		}
		got, dumpFileError := dumpFile(path, string(source))
		if dumpFileError != nil {
			different++
			test.Errorf("%s: the fork parses it and the converter refused: %v", path, dumpFileError)
			continue
		}
		for _, key := range []string{"ast", "comments"} {
			if difference := firstDifference(want[key], got[key], key); difference != "" {
				different++
				if different <= 10 {
					test.Errorf("%s: %s", path, difference)
				}
				break
			}
		}
		if difference := firstDifference(want["ast"], got["ast"], "ast") +
			firstDifference(want["comments"], got["comments"], "comments"); difference == "" {
			identical++
		}
	}
	test.Logf(
		"%d files, %d identical, %d different, %d the fork could not parse",
		len(files),
		identical,
		different,
		refused,
	)
	if refused > len(files)/10 {
		test.Errorf("the fork refused %d of %d files; the comparison is thinner than it looks", refused, len(files))
	}
}

// dumpFile converts one file and renders it the way the oracle script does, as the JSON values the oracle's
// line parses to, so both sides compare as the same values.
func dumpFile(path string, text string) (jsonvalue.JsonObject, error) {
	program, comments, _, parseTypeScriptError := ParseTypeScript(path, text, nil)
	if parseTypeScriptError != nil {
		return nil, parseTypeScriptError
	}
	offsets := utf16Offsets(text)
	dumped := make(jsonvalue.JsonArray, len(comments))
	for index, comment := range comments {
		dumped[index] = jsonvalue.JsonObject{
			"type": jsonString(comment.Type()),
			"range": jsonvalue.JsonArray{
				jsonInteger(offsets[comment.Range[0]]),
				jsonInteger(offsets[comment.Range[1]]),
			},
			"value": jsonString(comment.String("value")),
		}
	}
	return jsonvalue.JsonObject{"ast": dumpValue(program, offsets), "comments": dumped}, nil
}

// jsonString is a Go string as JSON carries it: encoding/json writes each byte that is not UTF-8 as U+FFFD,
// and converting to runes replaces each such byte the same way, so the dump reads as the round trip through
// JSON the oracle's side takes.
func jsonString(text string) jsonvalue.JsonValue {
	return jsonvalue.JsonString(string([]rune(text)))
}

// jsonInteger is an int as a JSON number.
func jsonInteger(number int) jsonvalue.JsonValue {
	return jsonvalue.JsonNumber(strconv.Itoa(number))
}

// firstDifference names the first path where two JSON values differ, or "".
func firstDifference(want jsonvalue.JsonValue, got jsonvalue.JsonValue, path string) string {
	switch typed := want.(type) {
	case jsonvalue.JsonObject:
		other, isMap := got.(jsonvalue.JsonObject)
		if !isMap {
			return fmt.Sprintf("%s: want an object, got %v", path, got)
		}
		keys := map[string]bool{}
		for key := range typed {
			keys[key] = true
		}
		for key := range other {
			keys[key] = true
		}
		sorted := make([]string, 0, len(keys))
		for key := range keys {
			sorted = append(sorted, key)
		}
		sort.Strings(sorted)
		for _, key := range sorted {
			wantValue, wantPresent := typed[key]
			gotValue, gotPresent := other[key]
			if wantPresent != gotPresent {
				return fmt.Sprintf(
					"%s.%s: present in the fork %v, in Go %v (%s)",
					path,
					key,
					wantPresent,
					gotPresent,
					typed["type"],
				)
			}
			if difference := firstDifference(wantValue, gotValue, path+"."+key); difference != "" {
				return difference
			}
		}
		return ""
	case jsonvalue.JsonArray:
		other, isList := got.(jsonvalue.JsonArray)
		if !isList || len(other) != len(typed) {
			return fmt.Sprintf("%s: want %d elements, got %v", path, len(typed), got)
		}
		for index := range typed {
			if difference := firstDifference(
				typed[index],
				other[index],
				fmt.Sprintf("%s[%d]", path, index),
			); difference != "" {
				return difference
			}
		}
		return ""
	}
	if scalarText(want) != scalarText(got) {
		return fmt.Sprintf("%s: want %v, got %v", path, scalarText(want), scalarText(got))
	}
	return ""
}

// scalarText is a JSON value as fmt prints the Go value encoding/json decodes it to, which is what the
// comparison has always been: a number by its float64, so `1.0` and `1` agree, and null as `<nil>`.
func scalarText(value jsonvalue.JsonValue) string {
	switch typed := value.(type) {
	case jsonvalue.JsonNull:
		return "<nil>"
	case jsonvalue.JsonBoolean:
		return strconv.FormatBool(bool(typed))
	case jsonvalue.JsonString:
		return string(typed)
	case jsonvalue.JsonNumber:
		// Ignored: a number out of float64's range parses to an infinity, which prints as one, and every
		// JsonNumber here was a number literal.
		number, _ := strconv.ParseFloat(string(typed), 64)
		return fmt.Sprint(number)
	case jsonvalue.JsonObject:
		return fmt.Sprintf("an object %v", typed)
	case jsonvalue.JsonArray:
		return fmt.Sprintf("an array %v", typed)
	}
	return "<missing>"
}

// utf16Offsets maps every byte offset that starts a character (and the end) to its UTF-16 index.
func utf16Offsets(text string) []int {
	offsets := make([]int, len(text)+1)
	index := 0
	for position, character := range text {
		offsets[position] = index
		size := utf8.RuneLen(character)
		for continuation := 1; continuation < size; continuation++ {
			offsets[position+continuation] = index
		}
		index += len(utf16.Encode([]rune{character}))
	}
	offsets[len(text)] = index
	return offsets
}

// dumpValue is a property's value in the shape the oracle script dumps it.
func dumpValue(value Value, offsets []int) jsonvalue.JsonValue {
	switch typed := value.(type) {
	case nil:
		return jsonvalue.JsonNull{}
	case *Node:
		if typed == nil {
			return jsonvalue.JsonNull{}
		}
		result := jsonvalue.JsonObject{
			"type":  jsonString(typed.Type()),
			"range": jsonvalue.JsonArray{jsonInteger(offsets[typed.Range[0]]), jsonInteger(offsets[typed.Range[1]])},
		}
		if typed.HasContentEnd {
			result["__contentEnd"] = jsonInteger(offsets[typed.ContentEnd])
		}
		keys := typed.Keys()
		sort.Strings(keys)
		for _, key := range keys {
			if key == "comments" || key == "tokens" || strings.HasPrefix(key, "__") {
				continue
			}
			result[key] = dumpValue(typed.Get(key), offsets)
		}
		return result
	case Nodes:
		result := make(jsonvalue.JsonArray, len(typed))
		for index, child := range typed {
			result[index] = dumpValue(child, offsets)
		}
		return result
	case Values:
		result := make(jsonvalue.JsonArray, len(typed))
		for index, element := range typed {
			result[index] = dumpValue(element, offsets)
		}
		return result
	case Record:
		result := make(jsonvalue.JsonObject, len(typed))
		for key, member := range typed {
			result[key] = dumpValue(member, offsets)
		}
		return result
	case *TemplateValue:
		var cooked jsonvalue.JsonValue = jsonvalue.JsonNull{}
		if typed.Cooked != nil {
			cooked = jsonString(*typed.Cooked)
		}
		return jsonvalue.JsonObject{"cooked": cooked, "raw": jsonString(typed.Raw)}
	case *Regex:
		return jsonvalue.JsonObject{"flags": jsonString(typed.Flags), "pattern": jsonString(typed.Pattern)}
	case String:
		return jsonString(string(typed))
	case Boolean:
		return jsonvalue.JsonBoolean(typed)
	case Number:
		// NaN and the infinities have no JSON spelling; encoding/json refused them where this dump went
		// through it, and json.Marshal still refuses this JsonNumber, so a convertedDump fails alike.
		return jsonvalue.JsonNumber(strconv.FormatFloat(float64(typed), 'g', -1, 64))
	case Integer:
		return jsonInteger(int(typed))
	}
	// Invariant: the cases above are every type that declares isValue, which only this package can.
	panic(fmt.Sprintf("estree: a property value of type %T", value))
}
