package sql_text_test

import (
	"slices"
	"testing"

	"github.com/system-inc/cohere/internal/sql_text"
)

// ReadStatement is pinned where it lives, one case per shape both rules lean on: a hole inside a quoted value, a
// hole bare after a comparison, the holes that build SQL from code and so are neither, and the readings it gives up
// on rather than guess.
func TestReadStatement(test *testing.T) {
	test.Parallel()
	cases := []struct {
		name         string
		texts        []string
		quotedSpans  []int
		bareSpans    []int
		keywordCount int
		certain      bool
	}{
		{
			name:         "a hole inside a quoted value",
			texts:        []string{"SELECT id FROM users WHERE name = '", "'"},
			quotedSpans:  []int{0},
			keywordCount: 3,
			certain:      true,
		},
		{
			name:         "a hole inside a LIKE pattern, after a doubled quote",
			texts:        []string{"SELECT id FROM users WHERE note = 'it''s' AND name LIKE '%", "%'"},
			quotedSpans:  []int{0},
			keywordCount: 5,
			certain:      true,
		},
		{
			name:         "a hole bare after a comparison",
			texts:        []string{"SELECT name FROM users WHERE id >= ", " LIMIT 10"},
			bareSpans:    []int{0},
			keywordCount: 4,
			certain:      true,
		},
		{
			name:         "a table qualifying a column after a comparison is a name, not a value",
			texts:        []string{"SELECT users.id FROM users JOIN t ON users.id = ", ".user_id"},
			keywordCount: 4,
			certain:      true,
		},
		{
			name:         "a table, a placeholder list and a name in quotes are code, not values",
			texts:        []string{"SELECT \"", "\" FROM ", " WHERE id IN (", ")"},
			keywordCount: 4,
			certain:      true,
		},
		{
			name:         "a quote that opens where no value goes",
			texts:        []string{"SELECT id FROM users AS '", "'"},
			keywordCount: 3,
			certain:      true,
		},
		{
			name:  "a backslash in quoted text, an escape on MySQL only",
			texts: []string{"SELECT id FROM users WHERE name = 'a\\", "'"},
		},
		{
			name:  "a comment opened by # on MySQL only",
			texts: []string{"SELECT id FROM users # note\nWHERE name = '", "'"},
		},
		{
			name:  "a text that ends inside a string",
			texts: []string{"SELECT id FROM users WHERE name = '", ""},
		},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			statement, certain := sql_text.ReadStatement(testCase.texts)
			if certain != testCase.certain {
				test.Fatalf("certain is %t, want %t", certain, testCase.certain)
			}
			if !slices.Equal(statement.QuotedSpans, testCase.quotedSpans) {
				test.Errorf("quoted spans are %v, want %v", statement.QuotedSpans, testCase.quotedSpans)
			}
			if !slices.Equal(statement.BareSpans, testCase.bareSpans) {
				test.Errorf("bare spans are %v, want %v", statement.BareSpans, testCase.bareSpans)
			}
			if statement.KeywordCount != testCase.keywordCount {
				test.Errorf("keyword count is %d, want %d", statement.KeywordCount, testCase.keywordCount)
			}
		})
	}
}
