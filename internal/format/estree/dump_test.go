package estree

import (
	"bufio"
	"encoding/json"
	"os"
	"sort"
	"strings"
	"testing"
	"unicode/utf16"
	"unicode/utf8"
)

// TestDump writes the converted tree of every file listed in COHERE_ESTREE_DUMP_FILES (one path per
// line) as JSON lines to COHERE_ESTREE_DUMP_OUT, in the shape the fork-side dumper prints, so the two
// trees can be diffed property by property. A measuring run, off by default.
//
// Offsets are converted to UTF-16 indexes for the comparison, because that is what the fork prints;
// the tree itself keeps bytes.
func TestDump(t *testing.T) {
	list := os.Getenv("COHERE_ESTREE_DUMP_FILES")
	if list == "" {
		t.Skip("set COHERE_ESTREE_DUMP_FILES and COHERE_ESTREE_DUMP_OUT to dump converted trees")
	}
	listFile, err := os.Open(list)
	if err != nil {
		t.Fatal(err)
	}
	defer listFile.Close()
	out, err := os.Create(os.Getenv("COHERE_ESTREE_DUMP_OUT"))
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	writer := bufio.NewWriter(out)
	defer writer.Flush()

	scanner := bufio.NewScanner(listFile)
	for scanner.Scan() {
		path := strings.TrimSpace(scanner.Text())
		if path == "" {
			continue
		}
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		text := string(source)
		offsets := utf16Offsets(text)
		record := map[string]any{"file": path}
		program, comments, err := ParseTypeScript(path, text)
		if err != nil {
			record["error"] = err.Error()
		} else {
			record["ast"] = dumpValue(program, offsets)
			dumped := make([]any, len(comments))
			for index, comment := range comments {
				dumped[index] = map[string]any{
					"type":  comment.Type(),
					"range": []int{offsets[comment.Range[0]], offsets[comment.Range[1]]},
					"value": comment.String("value"),
				}
			}
			record["comments"] = dumped
		}
		line, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		writer.Write(line)
		writer.WriteByte('\n')
	}
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

func dumpValue(value any, offsets []int) any {
	switch typed := value.(type) {
	case nil:
		return nil
	case *Node:
		if typed == nil {
			return nil
		}
		result := map[string]any{"type": typed.Type(), "range": []int{offsets[typed.Range[0]], offsets[typed.Range[1]]}}
		if typed.HasContentEnd {
			result["__contentEnd"] = offsets[typed.ContentEnd]
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
	case []*Node:
		result := make([]any, len(typed))
		for index, child := range typed {
			result[index] = dumpValue(child, offsets)
		}
		return result
	case *TemplateValue:
		var cooked any
		if typed.Cooked != nil {
			cooked = *typed.Cooked
		}
		return map[string]any{"cooked": cooked, "raw": typed.Raw}
	case *Regex:
		return map[string]any{"flags": typed.Flags, "pattern": typed.Pattern}
	default:
		return typed
	}
}
