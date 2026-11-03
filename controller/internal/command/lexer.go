package command

import (
	"fmt"
	"strings"
)

// Token spans are byte offsets in the original input, including quotes/escapes.
type Token struct {
	Value         string
	Start, End    int
	Pipe, Literal bool
}

type Lexed struct {
	Tokens  []Token
	Quote   rune
	Escaped bool
}

// Lex is shared by execution, pipelines and cursor help. Partial input is never executed.
func Lex(line string, partial bool) (Lexed, error) {
	var out Lexed
	var value strings.Builder
	start := -1
	literal := false
	flush := func(end int) {
		if start >= 0 {
			out.Tokens = append(out.Tokens, Token{Value: value.String(), Start: start, End: end, Literal: literal})
			value.Reset()
			start = -1
			literal = false
		}
	}
	for offset, r := range line {
		if out.Escaped {
			value.WriteRune(r)
			out.Escaped = false
			continue
		}
		if r == '\\' {
			if start < 0 {
				start = offset
			}
			literal = true
			out.Escaped = true
			continue
		}
		if out.Quote != 0 {
			if r == out.Quote {
				out.Quote = 0
			} else {
				value.WriteRune(r)
			}
			continue
		}
		switch r {
		case '\'', '"':
			if start < 0 {
				start = offset
			}
			literal = true
			out.Quote = r
		case ' ', '\t', '\r', '\n':
			flush(offset)
		case '|':
			flush(offset)
			out.Tokens = append(out.Tokens, Token{Value: "|", Start: offset, End: offset + 1, Pipe: true})
		default:
			if start < 0 {
				start = offset
			}
			value.WriteRune(r)
		}
	}
	flush(len(line))
	if !partial {
		if out.Escaped {
			return out, fmt.Errorf("trailing escape")
		}
		if out.Quote != 0 {
			return out, fmt.Errorf("unterminated quote")
		}
	}
	return out, nil
}

func HelpPosition(prefix string) bool {
	lexed, _ := Lex(prefix, true)
	return lexed.Quote == 0 && !lexed.Escaped
}

// CompletionEnd includes the rest of the current keyword, but never a literal.
func CompletionEnd(line string, cursor int) (int, bool) {
	if cursor < 0 || cursor > len(line) {
		return 0, false
	}
	lexed, _ := Lex(line, true)
	for _, token := range lexed.Tokens {
		if token.Start < cursor && cursor < token.End {
			return token.End, !token.Literal
		}
	}
	return cursor, true
}
