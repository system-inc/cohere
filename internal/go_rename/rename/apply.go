package rename

import (
	"bytes"
	"fmt"
	"go/format"
	"os"
	"sort"
)

// EditKey identifies an edit's span.
type EditKey struct {
	Filename string
	Offset   int
}

// Deduplicate merges edits proposed by several package variants. Identical edits collapse; two edits of the
// same span with different text are both dropped and reported.
func Deduplicate(edits []Edit) ([]Edit, []string) {
	byKey := map[EditKey][]Edit{}
	for _, edit := range edits {
		key := EditKey{Filename: edit.Filename, Offset: edit.Offset}
		byKey[key] = append(byKey[key], edit)
	}
	var kept []Edit
	var disagreements []string
	for key, group := range byKey {
		agree := true
		for _, edit := range group[1:] {
			if edit != group[0] {
				agree = false
			}
		}
		if !agree {
			disagreements = append(
				disagreements,
				fmt.Sprintf("%s offset %d: variants disagree: %v", key.Filename, key.Offset, group),
			)
			continue
		}
		kept = append(kept, group[0])
	}
	sort.Slice(kept, func(left, right int) bool {
		if kept[left].Filename != kept[right].Filename {
			return kept[left].Filename < kept[right].Filename
		}
		return kept[left].Offset < kept[right].Offset
	})
	sort.Strings(disagreements)
	return kept, disagreements
}

// RewriteFile applies one file's edits bottom-up and formats the result. It refuses overlapping edits and an
// edit whose old text is not what the file holds.
func RewriteFile(filename string, edits []Edit) (original []byte, rewritten []byte, rewriteError error) {
	original, readError := os.ReadFile(filename)
	if readError != nil {
		return nil, nil, readError
	}
	rewritten, rewriteError = RewriteSource(filename, original, edits)
	if rewriteError != nil {
		return nil, nil, rewriteError
	}
	return original, rewritten, nil
}

// RewriteSource is RewriteFile over original, the file's text as the load the edits came from read it, which may be a
// rewrite still in memory rather than what is on disk. filename only names the file in an error.
func RewriteSource(filename string, original []byte, edits []Edit) ([]byte, error) {
	sorted := append([]Edit(nil), edits...)
	sort.Slice(sorted, func(left, right int) bool { return sorted[left].Offset > sorted[right].Offset })
	result := append([]byte(nil), original...)
	previousStart := len(result) + 1
	for _, edit := range sorted {
		end := edit.Offset + len(edit.OldText)
		if end > previousStart {
			return nil, fmt.Errorf("%s: overlapping edits at offset %d", filename, edit.Offset)
		}
		if end > len(result) || !bytes.Equal(result[edit.Offset:end], []byte(edit.OldText)) {
			return nil, fmt.Errorf("%s: offset %d does not hold %q", filename, edit.Offset, edit.OldText)
		}
		result = append(result[:edit.Offset], append([]byte(edit.NewText), result[end:]...)...)
		previousStart = edit.Offset
	}
	formatted, formatError := format.Source(result)
	if formatError != nil {
		return nil, fmt.Errorf("%s: formatting: %w", filename, formatError)
	}
	return formatted, nil
}

// GroupByFile groups edits by filename.
func GroupByFile(edits []Edit) map[string][]Edit {
	byFile := map[string][]Edit{}
	for _, edit := range edits {
		byFile[edit.Filename] = append(byFile[edit.Filename], edit)
	}
	return byFile
}
