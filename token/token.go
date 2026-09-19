package token

type TokenType string

type Token struct {
	Type    TokenType
	Literal string
	Line    int
}

var keywords = map[string]TokenType{
	"fn":     FUNCTION,
	"let":    LET,
	"true":   TRUE,
	"false":  FALSE,
	"if":     IF,
	"else":   ELSE,
	"for":    FOR,
	"return": RETURN,
	"range":  RANGE,
	"break":  BREAK,
	"struct": STRUCT,
	"new":    NEW,
	"import": IMPORT,
	"pub":    PUB,
	"as":     AS,
	"null":   NULL,
}

func LookupIdent(ident string) TokenType {
	if tok, ok := keywords[ident]; ok {
		return tok
	}
	return IDENT
}

const (
	ILLEGAL = "ILLEGAL"
	EOF     = "EOF"

	STRING = "STRING"

	// Identifiers + literals
	IDENT = "IDENT" // add, foobar, x, y, ...
	INT   = "INT"   // 1343456
	FLOAT = "FLOAT"
	// Operators
	ASSIGN      = "="
	PLUS        = "+"
	PLUSASSIGN  = "+="
	MINUSASSIGN = "-="
	MULTASSIGN  = "*="
	DIVASSIGN   = "/="
	MINUS       = "-"
	BANG        = "!"
	ELLIPSIS    = "..."
	ASTERISK    = "*"
	SLASH       = "/"
	AND         = "&&"
	OR          = "||"

	NEWLINE = "\n"

	LT   = "<"
	GT   = ">"
	LTEQ = "<="
	GTEQ = ">="

	EQ     = "=="
	NOT_EQ = "!="

	// Delimiters
	COMMA     = ","
	SEMICOLON = ";"
	COLON     = ":"
	DOT       = "."

	COMMENT = "//"

	LPAREN   = "("
	RPAREN   = ")"
	LBRACE   = "{"
	RBRACE   = "}"
	LBRACKET = "["
	RBRACKET = "]"

	// Keywords
	NULL     = "NULL"
	FUNCTION = "FUNCTION"
	LET      = "LET"
	TRUE     = "TRUE"
	FALSE    = "FALSE"
	IF       = "IF"
	ELSE     = "ELSE"
	RETURN   = "RETURN"
	FOR      = "FOR"
	RANGE    = "RANGE"
	BREAK    = "BREAK"
	STRUCT   = "STRUCT"
	NEW      = "NEW"
	IMPORT   = "IMPORT"
	PUB      = "PUB"
	AS       = "AS"
)
