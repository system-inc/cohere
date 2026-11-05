// Package sqltext reads SQL written as text with holes in it, a value interpolated between each two texts, and
// says which holes sit where a value's own text becomes SQL: inside a single-quoted string, or bare after a
// comparison.
//
// Two engines ask it the same question. The TypeScript rule nexus/security-no-interpolated-sql-string reads a
// template literal's texts, and the Go rule cohere-go/security-no-interpolated-sql-string reads a query built with
// `+` and fmt.Sprintf. It moved out of package nexus so the Go rule reads SQL exactly as the TypeScript one does
// without linking the TypeScript checker, and it imports only `strings`, so it is a leaf. Swift's twin ports the
// same reading line for line.
package sqltext

import "strings"

// Statement is what ReadStatement found in a statement it could read with certainty.
type Statement struct {
	// QuotedSpans are the holes inside a single-quoted string whose opening quote stands where a value goes, by
	// the index of the text before each.
	QuotedSpans []int
	// BareSpans are the holes outside any string, right after a comparison, by the index of the text before each.
	BareSpans []int
	// KeywordCount is how many uppercase SQL keywords stand outside the statement's strings and comments. A reader
	// that has to decide whether text is SQL at all counts on it; one that already knows can ignore it.
	KeywordCount int
}

// keywords are the uppercase words that count as SQL when a statement is confirmed by a second keyword outside
// its strings and comments.
var keywords = map[string]bool{
	"SELECT": true, "INSERT": true, "UPDATE": true, "DELETE": true, "REPLACE": true, "WITH": true,
	"SHOW": true, "EXPLAIN": true, "PRAGMA": true, "WHERE": true, "AND": true, "OR": true, "SET": true,
	"VALUES": true, "HAVING": true, "FROM": true, "INTO": true, "LIKE": true, "GLOB": true,
	"BETWEEN": true, "JOIN": true, "ON": true, "IN": true, "IS": true, "NOT": true, "NULL": true,
	"ORDER": true, "GROUP": true, "BY": true, "LIMIT": true, "OFFSET": true, "AS": true,
	"DISTINCT": true, "COUNT": true, "TABLE": true, "STATUS": true, "CASE": true, "WHEN": true,
	"THEN": true, "ELSE": true, "END": true, "UNION": true, "ESCAPE": true, "ASC": true, "DESC": true,
}

// valueTokens are the tokens after which an opening quote begins a value: comparisons, concatenation, an
// argument or list position, and the keywords that take a value.
var valueTokens = map[string]bool{
	"=": true, "==": true, "<": true, ">": true, "<=": true, ">=": true, "<>": true, "!=": true,
	"||": true, "+": true, "(": true, ",": true,
	"LIKE": true, "GLOB": true, "REGEXP": true, "RLIKE": true, "BETWEEN": true, "AND": true,
	"SELECT": true, "WHEN": true, "THEN": true, "ELSE": true, "ESCAPE": true, "DEFAULT": true,
	"INTERVAL": true,
}

// comparisons are the operators after which an unquoted hole is a value compared, not a name or a fragment of
// SQL. After `(`, `,`, `AND`, `FROM` or `ORDER BY` a hole builds SQL from code (an `IN (?, ?)` list, a condition,
// a table or a column), and reading those would be false findings.
var comparisons = map[string]bool{
	"=": true, "==": true, "<": true, ">": true, "<=": true, ">=": true, "<>": true, "!=": true,
}

// IsWordCharacter is a character of a SQL name or keyword.
func IsWordCharacter(character byte) bool {
	return character == '_' || (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
		(character >= '0' && character <= '9')
}

// WordLength is the length of the word starting at start.
func WordLength(text string, start int) int {
	end := start
	for end < len(text) && IsWordCharacter(text[end]) {
		end++
	}
	return end - start
}

// IsWhitespace is a character SQL reads as space between tokens.
func IsWhitespace(character byte) bool {
	return character == ' ' || character == '\t' || character == '\r' || character == '\n'
}

// state is where a SQL reading stands between two characters.
type state int

const (
	inCode state = iota
	inString
	inDoubleQuotes
	inBackquotes
	inLineComment
	inBlockComment
)

// ReadStatement reads texts as one SQL statement, with each hole an opaque value between two texts, and reports
// whether the reading is certain. It is not, and finds nothing, when quoted text holds a backslash (an escape in
// MySQL, an ordinary character in SQLite and standard SQL, so where the string ends depends on a dialect the text
// does not name), on `#` outside a string (a comment in MySQL only), on `--` not followed by whitespace (MySQL
// reads `a--1` as arithmetic), on a `$$` or `$tag$` dollar quote, and when the text ends inside a string, a name
// or a block comment, a fragment whose other half the reader cannot see.
func ReadStatement(texts []string) (Statement, bool) {
	current := inCode
	previousToken := ""
	openedAtValue := false
	statement := Statement{}
	for textIndex, text := range texts {
		for index := 0; index < len(text); {
			character := text[index]
			switch current {
			case inCode:
				switch {
				case IsWhitespace(character):
					index++
				case character == '\'':
					current = inString
					openedAtValue = valueTokens[previousToken]
					index++
				case character == '"':
					current = inDoubleQuotes
					index++
				case character == '`':
					current = inBackquotes
					index++
				case character == '-' && index+1 < len(text) && text[index+1] == '-':
					if index+2 >= len(text) || !IsWhitespace(text[index+2]) {
						return Statement{}, false
					}
					current = inLineComment
					index += 3
				case character == '/' && index+1 < len(text) && text[index+1] == '*':
					current = inBlockComment
					index += 2
				case character == '#':
					return Statement{}, false
				case character == '$':
					// `$1` and `$name` are placeholders; `$$` and `$tag$` open a dollar-quoted string.
					length := WordLength(text, index+1)
					if index+1+length < len(text) && text[index+1+length] == '$' {
						return Statement{}, false
					}
					previousToken = "$"
					index += 1 + length
				case IsWordCharacter(character):
					length := WordLength(text, index)
					word := text[index : index+length]
					if keywords[word] {
						statement.KeywordCount++
					}
					previousToken = strings.ToUpper(word)
					index += length
				case strings.IndexByte("=<>!|", character) >= 0:
					end := index
					for end < len(text) && strings.IndexByte("=<>!|", text[end]) >= 0 {
						end++
					}
					previousToken = text[index:end]
					index = end
				default:
					previousToken = string(character)
					index++
				}
			case inString:
				switch character {
				case '\\':
					return Statement{}, false
				case '\'':
					if index+1 < len(text) && text[index+1] == '\'' {
						index += 2
						continue
					}
					current = inCode
					previousToken = "'"
				}
				index++
			case inDoubleQuotes, inBackquotes:
				closing := byte('"')
				if current == inBackquotes {
					closing = '`'
				}
				switch character {
				case '\\':
					return Statement{}, false
				case closing:
					if index+1 < len(text) && text[index+1] == closing {
						index += 2
						continue
					}
					current = inCode
					previousToken = string(closing)
				}
				index++
			case inLineComment:
				if character == '\n' {
					current = inCode
				}
				index++
			case inBlockComment:
				if character == '*' && index+1 < len(text) && text[index+1] == '/' {
					current = inCode
					index += 2
					continue
				}
				index++
			}
		}
		if textIndex == len(texts)-1 {
			break
		}
		// The hole after this text.
		switch current {
		case inString:
			if openedAtValue {
				statement.QuotedSpans = append(statement.QuotedSpans, textIndex)
			}
		case inCode:
			if comparisons[previousToken] {
				statement.BareSpans = append(statement.BareSpans, textIndex)
			}
			previousToken = "${}"
		}
	}
	if current != inCode && current != inLineComment {
		return Statement{}, false
	}
	return statement, true
}
