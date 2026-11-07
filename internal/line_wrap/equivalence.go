package line_wrap

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"strings"
)

var positionType = reflect.TypeFor[token.Pos]()
var commentGroupType = reflect.TypeFor[*ast.CommentGroup]()

// ignoredFields are bookkeeping, not syntax: object resolution and the file's comment list, which the word
// comparison covers.
var ignoredFields = map[string]bool{"Obj": true, "Scope": true, "Unresolved": true, "Comments": true}

// Equivalent proves two sources are the same program. The ASTs must match exactly, ignoring only positions
// (though whether each position is present must match) and comments, and the comments must hold the same words
// in the same order, so a reflow can move words between lines and nothing else.
func Equivalent(before, after []byte) error {
	beforeFile, parseFileError := parser.ParseFile(
		token.NewFileSet(),
		"",
		before,
		parser.ParseComments|parser.SkipObjectResolution,
	)
	if parseFileError != nil {
		return fmt.Errorf("parse before: %w", parseFileError)
	}
	afterFile, parseFileError := parser.ParseFile(
		token.NewFileSet(),
		"",
		after,
		parser.ParseComments|parser.SkipObjectResolution,
	)
	if parseFileError != nil {
		return fmt.Errorf("parse after: %w", parseFileError)
	}
	if compareValuesError := compareValues(
		reflect.ValueOf(beforeFile),
		reflect.ValueOf(afterFile),
		"File",
	); compareValuesError != nil {
		return compareValuesError
	}
	beforeWords := commentWords(beforeFile)
	afterWords := commentWords(afterFile)
	if len(beforeWords) != len(afterWords) {
		return fmt.Errorf("comment words: %d before, %d after", len(beforeWords), len(afterWords))
	}
	for index := range beforeWords {
		if beforeWords[index] != afterWords[index] {
			return fmt.Errorf("comment word %d: %q became %q", index, beforeWords[index], afterWords[index])
		}
	}
	return nil
}

func commentWords(file *ast.File) []string {
	var words []string
	for _, group := range file.Comments {
		for _, comment := range group.List {
			text := comment.Text
			if strings.HasPrefix(text, "//") {
				text = text[2:]
			} else {
				text = strings.TrimSuffix(strings.TrimPrefix(text, "/*"), "*/")
			}
			words = append(words, strings.Fields(text)...)
		}
	}
	return words
}

func compareValues(before, after reflect.Value, path string) error {
	if before.Type() != after.Type() {
		return fmt.Errorf("%s: %s became %s", path, before.Type(), after.Type())
	}
	switch before.Kind() {
	case reflect.Interface, reflect.Pointer:
		if before.IsNil() || after.IsNil() {
			if before.IsNil() != after.IsNil() {
				return fmt.Errorf("%s: nil differs", path)
			}
			return nil
		}
		return compareValues(before.Elem(), after.Elem(), path)
	case reflect.Struct:
		for index := range before.NumField() {
			field := before.Type().Field(index)
			if ignoredFields[field.Name] || field.Type == commentGroupType {
				continue
			}
			fieldPath := path + "." + field.Name
			if field.Type == positionType {
				// A token.Pos is an int, so its field reads as one with no assertion to miss.
				beforePosition := token.Pos(before.Field(index).Int())
				afterPosition := token.Pos(after.Field(index).Int())
				if beforePosition.IsValid() != afterPosition.IsValid() {
					return fmt.Errorf("%s: presence differs", fieldPath)
				}
				continue
			}
			if compareValuesError := compareValues(
				before.Field(index),
				after.Field(index),
				fieldPath,
			); compareValuesError != nil {
				return compareValuesError
			}
		}
		return nil
	case reflect.Slice:
		if before.Len() != after.Len() {
			return fmt.Errorf("%s: length %d became %d", path, before.Len(), after.Len())
		}
		for index := range before.Len() {
			if compareValuesError := compareValues(
				before.Index(index),
				after.Index(index),
				fmt.Sprintf("%s[%d]", path, index),
			); compareValuesError != nil {
				return compareValuesError
			}
		}
		return nil
	case reflect.String, reflect.Int, reflect.Bool, reflect.Uint, reflect.Int64, reflect.Uint64:
		if !before.Equal(after) {
			return fmt.Errorf("%s: %v became %v", path, before.Interface(), after.Interface())
		}
		return nil
	}
	return fmt.Errorf("%s: unhandled kind %s", path, before.Kind())
}
