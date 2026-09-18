package lexer

import (
	"bolt/token"
	"strings"
)

type Lexer struct {
	input        string
	position     int
	readPosition int
	ch           byte
	line         int
}

func New(input string) *Lexer {
	l := &Lexer{
		input: input,
		line:  1,
	}
	l.readChar()
	return l
}

func (l *Lexer) readChar() {
	if l.readPosition >= len(l.input) {
		l.ch = 0
	} else {
		l.ch = l.input[l.readPosition]
	}

	l.position = l.readPosition
	l.readPosition += 1
}

func (l *Lexer) NextToken() token.Token {

	var tok token.Token
Outer:
	l.skipWhitespace()

	switch l.ch {
	case '=':
		if l.peekChar() == '=' {
			ch := l.ch
			l.readChar()
			literal := string(ch) + string(l.ch)
			tok = token.Token{Type: token.EQ, Literal: literal, Line: l.line}
		} else {
			tok = l.newToken(token.ASSIGN, l.ch)
		}
	case '+':
		tok = l.newToken(token.PLUS, l.ch)
	case '-':
		tok = l.newToken(token.MINUS, l.ch)
	case '!':
		if l.peekChar() == '=' {
			ch := l.ch
			l.readChar()
			literal := string(ch) + string(l.ch)
			tok = token.Token{Type: token.NOT_EQ, Literal: literal, Line: l.line}
		} else {
			tok = l.newToken(token.BANG, l.ch)
		}
	case '&':
		if l.peekChar() == '&' {
			ch := l.ch
			l.readChar()
			literal := string(ch) + string(l.ch)
			tok = token.Token{Type: token.AND, Literal: literal, Line: l.line}
		} else {
			tok = l.newToken(token.ILLEGAL, l.ch)
		}
	case '|':
		if l.peekChar() == '|' {
			ch := l.ch
			l.readChar()
			literal := string(ch) + string(l.ch)
			tok = token.Token{Type: token.OR, Literal: literal, Line: l.line}
		} else {
			tok = l.newToken(token.ILLEGAL, l.ch)
		}
	case '[':
		tok = l.newToken(token.LBRACKET, l.ch)
	case ']':
		tok = l.newToken(token.RBRACKET, l.ch)
	case ':':
		tok = l.newToken(token.COLON, l.ch)
	case '.':
		if l.peekChar() == '.' {
			l.readChar()
			if l.peekChar() == '.' {
				l.readChar()
				tok = token.Token{Type: token.ELLIPSIS, Literal: "...", Line: l.line}
			} else {
				tok = token.Token{Type: token.ILLEGAL, Literal: ".." + string(l.peekChar()), Line: l.line}
			}
		} else {
			tok = l.newToken(token.DOT, l.ch)
		}
	case '"':
		tok.Type = token.STRING
		tok.Literal = l.readString()
		tok.Line = l.line
	case '/':
		if l.peekChar() == '/' {
			l.skipComment()
			goto Outer
		} else {
			tok = l.newToken(token.SLASH, l.ch)
		}
	case '*':
		tok = l.newToken(token.ASTERISK, l.ch)
	case '<':
		if l.peekChar() == '=' {
			ch := l.ch
			l.readChar()
			literal := string(ch) + string(l.ch)
			tok = token.Token{Type: token.LTEQ, Literal: literal, Line: l.line}
		} else {
			tok = l.newToken(token.LT, l.ch)
		}
	case '>':
		if l.peekChar() == '=' {
			ch := l.ch
			l.readChar()
			literal := string(ch) + string(l.ch)
			tok = token.Token{Type: token.GTEQ, Literal: literal, Line: l.line}
		} else {
			tok = l.newToken(token.GT, l.ch)
		}
	case ';':
		tok = l.newToken(token.SEMICOLON, l.ch)
	case ',':
		tok = l.newToken(token.COMMA, l.ch)
	case '{':
		tok = l.newToken(token.LBRACE, l.ch)
	case '}':
		tok = l.newToken(token.RBRACE, l.ch)
	case '(':
		tok = l.newToken(token.LPAREN, l.ch)
	case ')':
		tok = l.newToken(token.RPAREN, l.ch)
	case 0:
		tok.Literal = ""
		tok.Line = l.line
		tok.Type = token.EOF
	default:
		if isLetter(l.ch) {
			tok.Literal = l.readIdentifier()
			tok.Type = token.LookupIdent(tok.Literal)
			tok.Line = l.line
			return tok
		} else if isDigit(l.ch) {
			tok.Type = token.INT
			tok.Line = l.line
			tok.Literal = l.readNumber()
			if strings.Contains(tok.Literal, ".") {
				tok.Type = token.FLOAT
			}
			return tok
		} else {
			tok = l.newToken(token.ILLEGAL, l.ch)
		}
	}

	l.readChar()
	return tok

}

func (l *Lexer) readString() string {
	str := ""

	for {
		l.readChar()

		if l.ch == '"' || l.ch == 0 {
			break
		}

		if l.ch == '\\' {
			l.readChar()

			switch l.ch {
			case 'n':
				str += "\n"
			case 't':
				str += "\t"
			case 'r':
				str += "\r"
			case '\\':
				str += "\\"
			case '"':
				str += "\""
			default:
				// Either error or preserve the escape
				str += "\\" + string(rune(l.ch))
			}

			continue
		}

		if l.ch == '\n' {
			l.line++
		}

		str += string(rune(l.ch))
	}

	return str
}

func (l *Lexer) readNumber() string {
	position := l.position
	for isDigit(l.ch) || l.ch == '.' {
		l.readChar()
	}
	return l.input[position:l.position]
}

func isDigit(ch byte) bool {
	return '0' <= ch && ch <= '9'
}

func (l *Lexer) skipComment() {
	for l.ch != '\n' && l.ch != 0 {
		l.readChar()
	}
}

func (l *Lexer) skipWhitespace() {
	for l.ch == ' ' || l.ch == '\t' || l.ch == '\n' || l.ch == '\r' {
		if l.ch == '\n' {
			l.line++
		}
		l.readChar()
	}
}

func (l *Lexer) peekChar() byte {
	if l.readPosition < len(l.input) {
		return l.input[l.readPosition]
	}
	return 0
}

func (l *Lexer) newToken(tokenType token.TokenType, ch byte) token.Token {
	return token.Token{
		Type:    tokenType,
		Literal: string(ch),
		Line:    l.line,
	}
}

func isLetter(ch byte) bool {
	return ('a' <= ch && ch <= 'z') || ('A' <= ch && ch <= 'Z') || ch == '_'
}

func (l *Lexer) readIdentifier() string {
	position := l.position
	for isLetter(l.ch) || isDigit(l.ch) {
		l.readChar()
	}
	return l.input[position:l.position]
}
