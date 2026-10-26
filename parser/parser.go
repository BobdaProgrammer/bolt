package parser

import (
	"fmt"
	"strconv"

	"github.com/BobdaProgrammer/bolt/ast"
	"github.com/BobdaProgrammer/bolt/lexer"
	"github.com/BobdaProgrammer/bolt/token"
)

type (
	prefixParseFn func() ast.Expression
	infixParseFn  func(ast.Expression) ast.Expression // parameter is left side
)

type ParserError struct {
	Message string
	Line    int
}

type Parser struct {
	l         *lexer.Lexer
	curToken  token.Token
	peekToken token.Token
	errors    []ParserError

	importEnds bool

	prefixParseFns map[token.TokenType]prefixParseFn
	infixParseFns  map[token.TokenType]infixParseFn
}

func (p *Parser) newError(line int, format string, args ...any) {
	p.errors = append(p.errors, ParserError{Message: fmt.Sprintf(format, args...), Line: line})
}

func New(l *lexer.Lexer) *Parser {
	p := &Parser{l: l, errors: []ParserError{}, importEnds: false}
	p.nextToken()
	p.nextToken()
	p.prefixParseFns = make(map[token.TokenType]prefixParseFn)
	p.registerPrefix(token.IDENT, p.parseIdentifier)
	p.registerPrefix(token.INT, p.parseIntegerLiteral)
	p.registerPrefix(token.FLOAT, p.parseFloatLiteral)
	p.registerPrefix(token.TRUE, p.parseBooleanLiteral)
	p.registerPrefix(token.FALSE, p.parseBooleanLiteral)
	p.registerPrefix(token.BANG, p.parsePrefixExpression)
	p.registerPrefix(token.MINUS, p.parsePrefixExpression)
	p.registerPrefix(token.ELLIPSIS, p.parsePrefixExpression)
	p.registerPrefix(token.LPAREN, p.parseGroupedExpression)
	p.registerPrefix(token.IF, p.parseIfExpression)
	p.registerPrefix(token.NULL, p.parseNull)
	p.registerPrefix(token.DEFER, p.parseDefer)
	p.registerPrefix(token.FUNCTION, p.parseFunctionLiteral)
	p.registerPrefix(token.STRING, p.parseStringLiteral)
	p.registerPrefix(token.LBRACKET, p.parseArrayLiteral)
	p.registerPrefix(token.LBRACE, p.parseHashLiteral)
	p.registerPrefix(token.FOR, p.parseForExpression)
	p.registerPrefix(token.NEW, p.parseStructInstantiation)
	p.registerPrefix(token.SWITCH, p.parseSwitchExpression)
	p.registerPrefix(token.UNTERMINATEDSTRING, p.unterminatedStringError)

	p.infixParseFns = make(map[token.TokenType]infixParseFn)
	p.registerInfix(token.PLUS, p.parseInfixExpression)
	p.registerInfix(token.MINUS, p.parseInfixExpression)
	p.registerInfix(token.AND, p.parseInfixExpression)
	p.registerInfix(token.OR, p.parseInfixExpression)
	p.registerInfix(token.SLASH, p.parseInfixExpression)
	p.registerInfix(token.ASTERISK, p.parseInfixExpression)
	p.registerInfix(token.MODULO, p.parseInfixExpression)
	p.registerInfix(token.EQ, p.parseInfixExpression)
	p.registerInfix(token.NOT_EQ, p.parseInfixExpression)
	p.registerInfix(token.LT, p.parseInfixExpression)
	p.registerInfix(token.GT, p.parseInfixExpression)
	p.registerInfix(token.LTEQ, p.parseInfixExpression)
	p.registerInfix(token.GTEQ, p.parseInfixExpression)
	p.registerInfix(token.LPAREN, p.parseCallExpression)
	p.registerInfix(token.LBRACKET, p.parseArrayCallExpression)
	p.registerInfix(token.DOT, p.parseFieldAccess)
	p.registerInfix(token.AS, p.parseStructTypeConversion)
	p.registerInfix(token.ASSIGN, p.parseAssignment)
	p.registerInfix(token.PLUSASSIGN, p.parseAssignmentInfix)
	p.registerInfix(token.MINUSASSIGN, p.parseAssignmentInfix)
	p.registerInfix(token.MULTASSIGN, p.parseAssignmentInfix)
	p.registerInfix(token.DIVASSIGN, p.parseAssignmentInfix)
	return p
}

func (p *Parser) unterminatedStringError() ast.Expression {
	p.newError(p.curToken.Line, "Error: Unterminated string - line %d", p.curToken.Line)
	return nil
}

func (p *Parser) parseDefer() ast.Expression {
	def := &ast.Defer{Token: p.curToken, LineNum: p.curToken.Line}
	p.nextToken()
	exp := p.parseExpression(LOWEST)
	def.Exp = exp
	return def
}

func (p *Parser) parseSwitchExpression() ast.Expression {
	sw := &ast.SwitchExpression{Token: p.curToken, LineNum: p.curToken.Line}
	p.nextToken()
	if p.curTokenIs(token.LBRACE) {
		sw.SwitchItem = &ast.Boolean{Token: p.curToken, LineNum: p.curToken.Line, Value: true}
	} else {
		sw.SwitchItem = p.parseExpression(LOWEST)
		if !p.expectPeek(token.LBRACE) {
			return nil
		}
	}
	cases := []*ast.SwitchCase{}
	p.nextToken()
	for p.curTokenIs(token.CASE) {
		cas := &ast.SwitchCase{Token: p.curToken, LineNum: p.curToken.Line}
		if p.peekTokenIs(token.COLON) {
			p.newError(p.curToken.Line, "Case in switch statement must have a value to check. cannot use: `case:` - line=%d", p.curToken.Line)
			return nil
		}
		p.nextToken()
		cas.Condition = p.parseExpression(LOWEST)
		if !p.expectPeek(token.COLON) {
			return nil
		}
		cas.Consequence = p.parseBlockStatement()
		cases = append(cases, cas)
	}
	sw.Cases = cases
	return sw
}

func (p *Parser) parseNull() ast.Expression {
	return &ast.Null{Token: p.curToken, LineNum: p.curToken.Line}
}

func (p *Parser) parseAssignmentInfix(left ast.Expression) ast.Expression {
	op := p.curToken.Literal
	p.nextToken()
	expr := p.parseExpression(LOWEST)
	infix := &ast.InfixExpression{Token: p.curToken, LineNum: p.curToken.Line, Left: left, Right: expr, Operator: string(op[0])}
	stmt := &ast.AssignExpression{Token: p.curToken, LineNum: p.curToken.Line}

	if ident, ok := left.(*ast.Identifier); ok {
		stmt.Left = ident
	} else if fa, ok := left.(*ast.FieldAccess); ok {
		stmt.Left = fa
	} else if arr, ok := left.(*ast.ArrayLiteral); ok {
		stmt.Left = arr
	} else if left != nil {
		p.newError(left.Line(), "Cannot use type %s on left side of assign expression - line=%d", left.String(), left.Line())
		return nil
	} else {
		p.newError(p.curToken.Line, "Cannot use that type on left side of assign expression - line=%d", p.curToken.Line)
	}

	stmt.Value = infix
	if p.peekTokenIs(token.SEMICOLON) {
		p.nextToken()
	}
	return stmt
}

func (p *Parser) parseAssignment(left ast.Expression) ast.Expression {

	stmt := &ast.AssignExpression{Token: p.curToken, LineNum: p.curToken.Line}

	if ident, ok := left.(*ast.Identifier); ok {
		stmt.Left = ident
	} else if fa, ok := left.(*ast.FieldAccess); ok {
		stmt.Left = fa
	} else if arr, ok := left.(*ast.ArrayLiteral); ok {
		stmt.Left = arr
	} else if left != nil {
		p.newError(left.Line(), "Cannot use type %s on left side of assign expression - line=%d", left.String(), left.Line())
		return nil
	} else {
		p.newError(p.curToken.Line, "Cannot use that type on left side of assign expression - line=%d", p.curToken.Line)
	}

	p.nextToken()

	stmt.Value = p.parseExpression(LOWEST)

	if p.peekTokenIs(token.SEMICOLON) {
		p.nextToken()
	}

	return stmt
}

func (p *Parser) parseStructTypeConversion(left ast.Expression) ast.Expression {
	structconversion := &ast.StructTypeConversion{Token: p.curToken, LineNum: p.curToken.Line}
	if id, ok := left.(*ast.Identifier); ok {
		structconversion.Left = id
	} else if fa, ok := left.(*ast.FieldAccess); ok {
		structconversion.Left = fa
	} else if si, ok := left.(*ast.StructInstantiation); ok {
		structconversion.Left = si
	} else {
		p.newError(left.Line(), "Cannot use %s on left side of struct conversion - line = %d", left.String(), left.Line())
		return nil
	}
	p.nextToken()
	structconversion.StType = p.parseExpression(LOWEST) //p.parseIdentifier().(*ast.Identifier)
	return structconversion
}

func (p *Parser) parsePubStatement() ast.Statement {
	pub := &ast.PubStatement{Token: p.curToken, LineNum: p.curToken.Line}
	p.nextToken()
	stmt := p.parseStatement()
	if stmt == nil {
		p.newError(p.curToken.Line, "Expected statement after 'pub'. line=%d", p.curToken.Line)
		return nil
	}
	pub.Stmt = stmt
	return pub
}

func (p *Parser) parseImport() ast.Statement {
	if p.importEnds {
		p.newError(p.curToken.Line, "Import statement must precede all statements - line=%d", p.curToken.Line)
		return nil
	}
	is := &ast.ImportStatement{Token: p.curToken, LineNum: p.curToken.Line}
	p.nextToken()
	importval := p.parseExpression(LOWEST)
	if importval == nil {
		p.newError(p.curToken.Line, "Expected string after import. line = %d", p.curToken.Line)
		return nil
	}
	importstr, ok := importval.(*ast.StringLiteral)
	if !ok {
		p.newError(p.curToken.Line, "Expected string after import, got=%s . line=%d", importval.String(), p.curToken.Line)
		return nil
	}
	is.Value = importstr.Value
	return is
}

func (p *Parser) parseFieldAccess(left ast.Expression) ast.Expression {
	fa := &ast.FieldAccess{Token: p.curToken, LineNum: p.curToken.Line, Left: left}
	p.nextToken()
	right := p.parseExpression(CALL)
	fa.Right = right
	return fa
}

func (p *Parser) parseStructInstantiation() ast.Expression {
	p.nextToken()
	left := p.parseExpression(LOWEST)
	if !p.expectPeek(token.LBRACE) {
		return nil
	}
	sti := &ast.StructInstantiation{Token: p.curToken, LineNum: p.curToken.Line, Left: left}
	fields := map[*ast.Identifier]ast.Expression{}
	if p.peekTokenIs(token.RBRACE) {
		p.nextToken()
		sti.Fields = fields
		return sti
	}
	for !p.peekTokenIs(token.EOF) {
		p.nextToken()
		ident := p.parseIdentifier().(*ast.Identifier)
		if !p.expectPeek(token.COLON) {
			p.newError(p.curToken.Line, "Expected colon in between struct value assignment. got=%s . line=%d", p.peekToken.Literal, p.curToken.Line)
			return nil
		}
		p.nextToken()
		val := p.parseExpression(LOWEST)
		fields[ident] = val
		if p.peekTokenIs(token.RBRACE) {
			p.nextToken()
			break
		} else if !p.expectPeek(token.COMMA) {
			return nil
		}

	}
	sti.Fields = fields
	return sti
}

func (p *Parser) parseStructType() ast.Statement {
	s := &ast.StructType{Token: p.curToken, LineNum: p.curToken.Line}
	p.nextToken()
	if p.curTokenIs(token.LBRACE) {
		p.newError(p.curToken.Line, "Struct types must be in format: struct <Identifier> {}. Identifier is missing. got { . line=%d", p.curToken.Line)
		return nil
	}
	s.Name = p.parseIdentifier().(*ast.Identifier)
	if !p.expectPeek(token.LBRACE) {
		return nil
	}
	p.nextToken()
	s.Fields = []*ast.Identifier{}
	if p.curTokenIs(token.RBRACE) {
		return s
	}
	for !p.peekTokenIs(token.RBRACE) || !p.peekTokenIs(token.EOF) {
		ident := p.parseIdentifier().(*ast.Identifier)
		s.Fields = append(s.Fields, ident)
		if p.peekTokenIs(token.RBRACE) {
			p.nextToken()
			break
		} else if !p.expectPeek(token.COMMA) {
			p.newError(p.curToken.Line, "Expected comma between struct field values. Line=%d", p.curToken.Line)
			return nil
		}
		p.nextToken()
	}
	return s
}

func (p *Parser) parseBreakStatement() ast.Statement {
	return &ast.BreakStatement{Token: p.curToken, LineNum: p.curToken.Line}
}

func (p *Parser) parseContinueStatement() ast.Statement {
	return &ast.ContinueStatement{Token: p.curToken, LineNum: p.curToken.Line}
}
func (p *Parser) parseForExpression() ast.Expression {
	f := &ast.ForExpression{Token: p.curToken, LineNum: p.curToken.Line}
	p.nextToken()
	tok := p.curToken
	if p.curTokenIs(token.LBRACE) {
		f.Condition = &ast.Boolean{Value: true}
	} else {
		f.Condition = p.parseExpression(LOWEST)
		if p.peekTokenIs(token.COMMA) || p.peekTokenIs(token.ASSIGN) {
			// for val = range arr
			p.nextToken()
			val1, ok := f.Condition.(*ast.Identifier)
			if !ok {
				p.newError(p.curToken.Line, "Expected identifier token in range expression. got %s . line=%d", p.curToken.Type, p.curToken.Line)
				return nil
			}
			exp := &ast.RangeExpression{Token: tok, Val1: val1}
			if p.curTokenIs(token.COMMA) {
				p.nextToken()
				//exp.Val2
				val2, ok := p.parseIdentifier().(*ast.Identifier)
				if ok {
					exp.Val2 = val2
				} else {
					p.newError(p.curToken.Line, "Expected identifier in range expression, got %s . line=%d", val2.Token.Type, p.curToken.Line)
					return nil
				}
				p.nextToken()
			}
			if !p.curTokenIs(token.ASSIGN) {
				p.newError(p.curToken.Line, "Expected equal token in range expression. got %s . line=%d", p.curToken.Type, p.curToken.Line)
				return nil
			}
			if !p.expectPeek(token.RANGE) {
				return nil
			}
			if p.peekTokenIs(token.LBRACE) {
				p.newError(p.curToken.Line, "Need an expression to range over inside of for loop range expression. got } . line=%d", p.curToken.Line)
				return nil
			}
			p.nextToken()
			exp.Ranging = p.parseExpression(LOWEST)
			if !p.expectPeek(token.LBRACE) {
				return nil
			}
			f.Condition = exp

		} else if !p.expectPeek(token.LBRACE) {
			return nil
		}
	}
	f.Consequence = p.parseBlockStatement()
	return f
}

// func (p *Parser) parseForExpression() ast.Expression {
// 	f := &ast.ForExpression{Token: p.curToken, LineNum:p.curToken.Line}
// 	p.nextToken()
// 	f.Condition = p.parseExpression(LOWEST)
// 	if !p.expectPeek(token.LBRACE) {
// 		return nil
// 	}
// 	f.Consequence = p.parseBlockStatement()
// 	return f
// }

func (p *Parser) parseHashLiteral() ast.Expression {
	hash := &ast.HashLiteral{Token: p.curToken, LineNum: p.curToken.Line}
	hash.Pairs = make(map[ast.Expression]ast.Expression)
	for !p.peekTokenIs(token.RBRACE) {
		p.nextToken()
		key := p.parseExpression(LOWEST)

		if !p.expectPeek(token.COLON) {
			return nil
		}

		p.nextToken()
		value := p.parseExpression(LOWEST)

		hash.Pairs[key] = value

		if !p.peekTokenIs(token.RBRACE) && !p.expectPeek(token.COMMA) {
			return nil
		}
	}

	if !p.expectPeek(token.RBRACE) {
		return nil
	}

	return hash
}

func (p *Parser) parseArrayCallExpression(left ast.Expression) ast.Expression {

	p.nextToken()
	firstVal := p.parseExpression(LOWEST)

	// Slice: myArr[1:2]
	if p.peekTokenIs(token.COLON) {
		exp := &ast.SliceExpression{Token: p.curToken, LineNum: p.curToken.Line, Left: left}
		exp.IndexStart = firstVal
		p.nextToken()
		p.nextToken()
		exp.IndexEnd = p.parseExpression(LOWEST)
		if !p.expectPeek(token.RBRACKET) {
			return nil
		}
		return exp
	} else if p.expectPeek(token.RBRACKET) {
		// Index: myArr[1]
		if p.peekTokenIs(token.ASSIGN) {
			p.nextToken()
			p.nextToken()
			val := p.parseExpression(LOWEST)
			exp := &ast.IndexAssignExpression{Token: p.curToken, LineNum: p.curToken.Line, Left: left, Index: firstVal, Value: val}
			return exp
		} else {
			exp := &ast.IndexExpression{Token: p.curToken, LineNum: p.curToken.Line, Left: left}
			exp.Index = firstVal
			return exp
		}
	} else {
		return nil
	}
}

func (p *Parser) parseArrayLiteral() ast.Expression {
	array := &ast.ArrayLiteral{Token: p.curToken, LineNum: p.curToken.Line}
	array.Elements = p.parseExpressionList(token.RBRACKET)
	return array
}

func (p *Parser) parseExpressionList(end token.TokenType) []ast.Expression {
	list := []ast.Expression{}

	if p.peekTokenIs(end) {
		p.nextToken()
		return list
	}

	p.nextToken()
	list = append(list, p.parseExpression(LOWEST))

	for p.peekTokenIs(token.COMMA) {
		p.nextToken()
		p.nextToken()
		list = append(list, p.parseExpression(LOWEST))
	}

	if !p.expectPeek(end) {
		return nil
	}

	return list
}

func (p *Parser) parseStringLiteral() ast.Expression {
	return &ast.StringLiteral{Token: p.curToken, LineNum: p.curToken.Line, Value: p.curToken.Literal}
}

func (p *Parser) parseCallExpression(function ast.Expression) ast.Expression {
	exp := &ast.CallExpression{Token: p.curToken, LineNum: p.curToken.Line, Function: function}
	exp.Arguments = p.parseExpressionList(token.RPAREN)
	return exp
}

func (p *Parser) parseFunctionParameters() []*ast.Identifier {
	identifiers := []*ast.Identifier{}

	if p.peekTokenIs(token.RPAREN) {
		p.nextToken()
		return identifiers
	}

	p.nextToken()

	ident := &ast.Identifier{Token: p.curToken, LineNum: p.curToken.Line, Value: p.curToken.Literal}
	identifiers = append(identifiers, ident)

	for p.peekTokenIs(token.COMMA) {
		p.nextToken()
		p.nextToken()
		ident := &ast.Identifier{Token: p.curToken, LineNum: p.curToken.Line, Value: p.curToken.Literal}
		identifiers = append(identifiers, ident)
	}

	if !p.expectPeek(token.RPAREN) {
		return nil
	}

	return identifiers
}

func (p *Parser) parseFunctionLiteral() ast.Expression {
	lit := &ast.FunctionLiteral{Token: p.curToken, LineNum: p.curToken.Line}
	if !p.expectPeek(token.LPAREN) {
		return nil
	}
	lit.Parameters = p.parseFunctionParameters()
	if !p.expectPeek(token.LBRACE) {
		return nil
	}

	lit.Body = p.parseBlockStatement()

	return lit
}

func (p *Parser) parseIfExpression() ast.Expression {
	expression := &ast.IfExpression{Token: p.curToken, LineNum: p.curToken.Line}

	p.nextToken()
	expression.Condition = p.parseExpression(LOWEST)

	if !p.expectPeek(token.LBRACE) {
		return nil
	}

	expression.Consequence = p.parseBlockStatement()

	if p.peekTokenIs(token.ELSE) {
		p.nextToken()
		// ELSE IF
		if p.peekTokenIs(token.IF) {
			curExp := &ast.IfExpression{}
			expression.Alternative = curExp
			// else if x == 3 {
			// code...
			//} else if x== 4{
			//code..
			//} else{
			//code
			//}
			for {
				curExp.Alternative = &ast.IfExpression{
					Token: p.curToken, LineNum: p.curToken.Line,
				}
				p.nextToken()
				if p.peekTokenIs(token.LBRACE) {
					p.newError(p.curToken.Line, "Else if needs condition, got { . line = %d", p.curToken.Line)
					return nil
				}
				p.nextToken()

				curExp.Condition = p.parseExpression(LOWEST)

				if !p.expectPeek(token.LBRACE) {
					return nil
				}

				curExp.Consequence = p.parseBlockStatement()

				if !p.peekTokenIs(token.ELSE) {
					curExp.Alternative = nil
					break
				}
				p.nextToken()
				if !p.peekTokenIs(token.IF) {
					if !p.expectPeek(token.LBRACE) {
						return nil
					}
					curExp.Alternative = &ast.IfExpression{
						Token:       p.curToken,
						Condition:   nil,
						Consequence: p.parseBlockStatement(),
						Alternative: nil,
					}
					break
				}

				curExp.Alternative = &ast.IfExpression{}
				curExp = curExp.Alternative

			}
		} else if !p.expectPeek(token.LBRACE) {
			return nil
		} else {
			expression.Alternative = &ast.IfExpression{
				Token:       p.curToken,
				Condition:   nil,
				Consequence: p.parseBlockStatement(),
				Alternative: nil,
			}
		}

	}

	return expression
}

func (p *Parser) parseBlockStatement() *ast.BlockStatement {
	block := &ast.BlockStatement{Token: p.curToken, LineNum: p.curToken.Line}
	block.Statements = []ast.Statement{}

	p.nextToken()

	for !p.curTokenIs(token.RBRACE) && !p.curTokenIs(token.EOF) && !p.curTokenIs(token.CASE) {
		stmt := p.parseStatement()
		if stmt != nil {
			block.Statements = append(block.Statements, stmt)
		}
		p.nextToken()
	}
	return block
}

func (p *Parser) parseGroupedExpression() ast.Expression {
	p.nextToken()
	exp := p.parseExpression(LOWEST)
	if !p.expectPeek(token.RPAREN) {
		return nil
	}
	return exp
}

func (p *Parser) parseBooleanLiteral() ast.Expression {
	return &ast.Boolean{Token: p.curToken, LineNum: p.curToken.Line, Value: p.curTokenIs(token.TRUE)}
}

func (p *Parser) parseInfixExpression(left ast.Expression) ast.Expression {
	// defer untrace(trace("parseInfixExpression"))
	expression := &ast.InfixExpression{
		Token:    p.curToken,
		Operator: p.curToken.Literal,
		Left:     left,
		LineNum:  p.curToken.Line,
	}

	precedence := p.curPrecedence()
	p.nextToken()
	expression.Right = p.parseExpression(precedence)

	return expression
}

func (p *Parser) parsePrefixExpression() ast.Expression {
	// defer untrace(trace("parsePrefixExpression"))
	expression := &ast.PrefixExpression{
		Token:    p.curToken,
		Operator: p.curToken.Literal,
	}

	p.nextToken()
	expression.Right = p.parseExpression(PREFIX)

	return expression
}

func (p *Parser) parseFloatLiteral() ast.Expression {
	lit := &ast.FloatLiteral{Token: p.curToken, LineNum: p.curToken.Line}

	val, err := strconv.ParseFloat(p.curToken.Literal, 64)
	if err != nil {
		p.newError(p.curToken.Line, "Could not parse %q as float. Line %d", p.curToken.Literal, p.curToken.Line)
		return nil
	}

	lit.Value = val

	return lit
}

func (p *Parser) parseIntegerLiteral() ast.Expression {
	// defer untrace(trace("parseIntegerLiteral"))
	lit := &ast.IntegerLiteral{Token: p.curToken, LineNum: p.curToken.Line}

	val, err := strconv.ParseInt(p.curToken.Literal, 0, 64)
	if err != nil {
		p.newError(p.curToken.Line, "Could not parse %q as integer. Line %d", p.curToken.Literal, p.curToken.Line)
		return nil
	}

	lit.Value = val
	return lit
}

func (p *Parser) parseIdentifier() ast.Expression {
	return &ast.Identifier{Token: p.curToken, LineNum: p.curToken.Line, Value: p.curToken.Literal}
}

func (p *Parser) registerPrefix(TokenType token.TokenType, fn prefixParseFn) {
	p.prefixParseFns[TokenType] = fn
}

func (p *Parser) registerInfix(TokenType token.TokenType, fn infixParseFn) {
	p.infixParseFns[TokenType] = fn
}

func (p *Parser) Errors() []string {
	errs := []string{}
	for _, e := range p.errors {
		errs = append(errs, e.Message)
	}
	return errs
}

func (p *Parser) ParserErrors() []ParserError {
	return p.errors
}

func (p *Parser) peekError(t token.TokenType) {
	p.newError(p.peekToken.Line, "expected next token to be ` %s `  got %s instead. Line %d", t, p.peekToken.Type, p.peekToken.Line)
}

func (p *Parser) nextToken() {
	p.curToken = p.peekToken
	p.peekToken = p.l.NextToken()
}

func (p *Parser) ParseProgram() *ast.Program {
	program := &ast.Program{}
	program.Statements = []ast.Statement{}

	for !p.curTokenIs(token.EOF) {
		stmt := p.parseStatement()
		if stmt != nil {
			if !p.importEnds {
				if _, ok := stmt.(*ast.ImportStatement); !ok {
					p.importEnds = true
				}
			}
			program.Statements = append(program.Statements, stmt)
		}
		p.nextToken()
	}

	return program
}

func (p *Parser) parseStatement() ast.Statement {
	switch p.curToken.Type {
	case token.LET:
		return p.parseLetStatement()
	case token.RETURN:
		return p.parseReturnStatement()
	case token.BREAK:
		return p.parseBreakStatement()
	case token.CONTINUE:
		return p.parseContinueStatement()
	case token.STRUCT:
		return p.parseStructType()
	case token.IMPORT:
		return p.parseImport()
	case token.PUB:
		return p.parsePubStatement()
	default:
		return p.parseExpressionStatement()
	}
}

const (
	_ int = iota
	PROGRAMLOWEST
	ASSIGN // =
	LOWEST
	AND         // &&
	OR          // ||
	EQUALS      // ==
	LESSGREATER // > or <
	SUM         // +
	MODULO      // %
	PRODUCT     // *
	AS          // AS
	PREFIX      // !X or -X
	CALL        // myFunc()
	INDEX       // myArray[0]
	FIELDACCESS // Val.field
)

var precedences = map[token.TokenType]int{
	token.ASSIGN:   ASSIGN,
	token.EQ:       EQUALS,
	token.NOT_EQ:   EQUALS,
	token.LT:       LESSGREATER,
	token.GT:       LESSGREATER,
	token.LTEQ:     LESSGREATER,
	token.GTEQ:     LESSGREATER,
	token.PLUS:     SUM,
	token.MINUS:    SUM,
	token.MODULO:   MODULO,
	token.SLASH:    PRODUCT,
	token.ASTERISK: PRODUCT,
	token.LPAREN:   CALL,
	token.LBRACKET: INDEX,
	token.DOT:      FIELDACCESS,
	token.AND:      AND,
	token.AS:       AS,
	token.OR:       OR,
}

func (p *Parser) curPrecedence() int {
	if p, ok := precedences[p.curToken.Type]; ok {
		return p
	}
	return LOWEST
}

func (p *Parser) peekPrecedence() int {
	if p, ok := precedences[p.peekToken.Type]; ok {
		return p
	}

	return LOWEST
}

func (p *Parser) parseExpressionStatement() *ast.ExpressionStatement {
	// defer untrace(trace("parseExpressionStatement"))
	stmt := &ast.ExpressionStatement{Token: p.curToken, LineNum: p.curToken.Line}
	stmt.Expression = p.parseExpression(PROGRAMLOWEST)

	if p.peekTokenIs(token.SEMICOLON) {
		p.nextToken()
	}

	return stmt
}

func (p *Parser) noPrefixParseFnError(t token.TokenType) {
	p.newError(p.curToken.Line, "Unexpected: %s found. Line %d", t, p.curToken.Line)
}
func (p *Parser) parseExpression(precedence int) ast.Expression {
	// defer untrace(trace("parseExpression"))
	prefix := p.prefixParseFns[p.curToken.Type]
	if prefix == nil {
		p.noPrefixParseFnError(p.curToken.Type)
		return nil
	}

	leftExp := prefix()

	for !p.peekTokenIs(token.SEMICOLON) && precedence < p.peekPrecedence() {
		infix := p.infixParseFns[p.peekToken.Type]
		if infix == nil {
			return leftExp
		}
		p.nextToken()
		leftExp = infix(leftExp)
	}

	return leftExp
}

func (p *Parser) parseReturnStatement() *ast.ReturnStatement {
	stmt := &ast.ReturnStatement{Token: p.curToken, LineNum: p.curToken.Line}

	p.nextToken()
	if p.curTokenIs(token.SEMICOLON) {
		p.nextToken()
		stmt.ReturnValues = []ast.Expression{}
		return stmt
	}

	first := p.parseExpression(LOWEST)
	values := []ast.Expression{first}
	for p.peekTokenIs(token.COMMA) {
		p.nextToken()
		p.nextToken()
		values = append(values, p.parseExpression(LOWEST))
	}

	stmt.ReturnValues = values

	if p.peekTokenIs(token.SEMICOLON) {
		p.nextToken()
	}

	return stmt
}

func (p *Parser) parseLetStatement() *ast.LetStatement {
	stmt := &ast.LetStatement{Token: p.curToken, LineNum: p.curToken.Line}

	p.nextToken()
	left := p.parseExpression(LOWEST)
	if ident, ok := left.(*ast.Identifier); ok {
		stmt.Left = ident
	} else if fa, ok := left.(*ast.FieldAccess); ok {
		stmt.Left = fa
	} else if arr, ok := left.(*ast.ArrayLiteral); ok {
		stmt.Left = arr
	} else if left != nil {
		p.newError(left.Line(), "Cannot use type %s on left side of let statement - line=%d", left.String(), left.Line())
		return nil
	} else {
		p.newError(p.curToken.Line, "Cannot use that type on left side of let statement - line=%d", p.curToken.Line)
	}

	if !p.expectPeek(token.ASSIGN) {
		p.nextToken()
		return nil
	}

	p.nextToken()

	stmt.Value = p.parseExpression(LOWEST)

	if p.peekTokenIs(token.SEMICOLON) {
		p.nextToken()
	}

	return stmt
}

func (p *Parser) curTokenIs(t token.TokenType) bool {
	return p.curToken.Type == t
}

func (p *Parser) peekTokenIs(t token.TokenType) bool {
	return p.peekToken.Type == t
}

func (p *Parser) expectPeek(t token.TokenType) bool {
	if p.peekTokenIs(t) {
		p.nextToken()
		return true
	} else {
		p.peekError(t)
		return false
	}
}
