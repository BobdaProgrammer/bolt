package repl

import (
	"bolt/ast"
	"bolt/evaluator"
	"bolt/lexer"
	"bolt/object"
	"bolt/parser"
	"bolt/token"
	"bufio"
	"fmt"
	"io"
	"strings"
)

const PROMPT = "~ $ "

func Start(in io.Reader, out io.Writer) {
	scanner := bufio.NewScanner(in)
	env := object.NewEnvironment()
	for {
		fmt.Fprint(out, PROMPT)
		scanned := scanner.Scan()
		if !scanned {
			return
		}

		line := scanner.Text()
		l := lexer.New(line)

		fmt.Fprint(out, "------------ LEXER -------------\n")

		for tok := l.NextToken(); tok.Type != token.EOF; tok = l.NextToken() {
			fmt.Fprintf(out, "%+v\n", tok)
		}

		fmt.Fprint(out, "\n\n----------- PARSER & AST ------------\n")
		l = lexer.New(line)
		p := parser.New(l)
		program := p.ParseProgram()
		if len(p.Errors()) != 0 {
			printParserErrors(out, p.Errors())
			continue
		}

		createAST(0, out, program.Statements)

		fmt.Fprint(out, "\n-- parser string --\n")

		io.WriteString(out, program.String())
		io.WriteString(out, "\n")

		fmt.Fprint(out, "\n---------- EVALUATOR ----------\n")

		evaluated := evaluator.Eval(program, env)
		if evaluated != nil {
			io.WriteString(out, fmt.Sprint(evaluated.Inspect(), "\n"))
		}

	}
}

func createAST(indent int, out io.Writer, stmts []ast.Statement) {
	for _, stmt := range stmts {
		fmt.Fprintf(out, "%+v\n", fmt.Sprint(strings.Repeat("    ", indent), fmt.Sprintf("%T", stmt)))
		if let, ok := stmt.(*ast.LetStatement); ok {
			indent++
			fmt.Fprintf(out, "%+v\n", fmt.Sprint(strings.Repeat("    ", indent), "Name: ", let.Name))
			fmt.Fprintf(out, "%+v\n", fmt.Sprint(strings.Repeat("    ", indent), "Value expression: ", fmt.Sprintf("%T", let.Value)))
			indent++
			expressionStatementAST(indent, out, &ast.ExpressionStatement{Expression: let.Value})
			indent -= 2
		}
		if ret, ok := stmt.(*ast.ReturnStatement); ok {
			indent++
			fmt.Fprintf(out, "%+v\n", fmt.Sprint(strings.Repeat("    ", indent), "Value expression: "))
			indent++
			expressionStatementAST(indent, out, &ast.ExpressionStatement{Expression: ret.ReturnValue})
		}
		if es, ok := stmt.(*ast.ExpressionStatement); ok {
			expressionStatementAST(indent, out, es)
		}
	}
}

func expressionStatementAST(indent int, out io.Writer, es *ast.ExpressionStatement) {
	if fn, ok := es.Expression.(*ast.FunctionLiteral); ok {
		indent++
		fmt.Fprintf(out, "%+v\n", fmt.Sprint(strings.Repeat("    ", indent), fmt.Sprintf("%T", fn)))
		indent++
		fmt.Fprintf(out, "%+v\n", fmt.Sprint(strings.Repeat("    ", indent), "Parameters: ", fn.Parameters))
		fmt.Fprintf(out, "%+v\n", fmt.Sprint(strings.Repeat("    ", indent), "Body:"))
		indent++
		createAST(indent, out, fn.Body.Statements)
		indent -= 3
	} else if f, ok := es.Expression.(*ast.IfExpression); ok {
		indent++
		fmt.Fprintf(out, "%+v\n", fmt.Sprint(strings.Repeat("    ", indent), fmt.Sprintf("%T", f)))
		indent++
		fmt.Fprintf(out, "%+v\n", fmt.Sprint(strings.Repeat("    ", indent), "Condition:"))
		indent++
		expressionStatementAST(indent, out, &ast.ExpressionStatement{Expression: f.Condition})
		indent--
		fmt.Fprintf(out, "%+v\n", fmt.Sprint(strings.Repeat("    ", indent), "Consequence:"))
		indent++
		createAST(indent, out, f.Consequence.Statements)
		indent--
		if f.Alternative != nil {
			fmt.Fprintf(out, "%+v\n", fmt.Sprint(strings.Repeat("    ", indent), "Alternative:"))
			indent++
			createAST(indent, out, f.Alternative.Statements)
			indent--
		}
		indent -= 2
	} else if ce, ok := es.Expression.(*ast.CallExpression); ok {
		indent++
		fmt.Fprintf(out, "%+v\n", fmt.Sprint(strings.Repeat("    ", indent), fmt.Sprintf("%T", ce)))
		indent++
		fmt.Fprintf(out, "%+v\n", fmt.Sprint(strings.Repeat("    ", indent), "Function:"))
		indent++
		expressionStatementAST(indent, out, &ast.ExpressionStatement{Expression: ce.Function})
		indent--
		if len(ce.Arguments) > 0 {
			fmt.Fprintf(out, "%+v\n", fmt.Sprint(strings.Repeat("    ", indent), "Arguments:"))
			indent++
			for _, arg := range ce.Arguments {
				expressionStatementAST(indent, out, &ast.ExpressionStatement{Expression: arg})
			}
			indent--
		}
		indent -= 2
	} else if pe, ok := es.Expression.(*ast.PrefixExpression); ok {
		indent++
		fmt.Fprintf(out, "%+v\n", fmt.Sprint(strings.Repeat("    ", indent), fmt.Sprintf("%T", pe)))
		indent++
		fmt.Fprintf(out, "%+v\n", fmt.Sprint(strings.Repeat("    ", indent), "Operator:", pe.Operator))
		fmt.Fprintf(out, "%+v\n", fmt.Sprint(strings.Repeat("    ", indent), "Right:"))
		indent++
		expressionStatementAST(indent, out, &ast.ExpressionStatement{Expression: pe.Right})
		indent -= 3
	} else if ie, ok := es.Expression.(*ast.InfixExpression); ok {
		indent++
		fmt.Fprintf(out, "%+v\n", fmt.Sprint(strings.Repeat("    ", indent), fmt.Sprintf("%T", ie)))
		indent++
		fmt.Fprintf(out, "%+v\n", fmt.Sprint(strings.Repeat("    ", indent), "Left:"))
		indent++
		expressionStatementAST(indent, out, &ast.ExpressionStatement{Expression: ie.Left})
		indent--
		fmt.Fprintf(out, "%+v\n", fmt.Sprint(strings.Repeat("    ", indent), "Operator:", ie.Operator))
		fmt.Fprintf(out, "%+v\n", fmt.Sprint(strings.Repeat("    ", indent), "Right:"))
		indent++
		expressionStatementAST(indent, out, &ast.ExpressionStatement{Expression: ie.Right})
		indent -= 2
	} else {
		fmt.Fprintf(out, "%+v\n", fmt.Sprint(strings.Repeat("    ", indent), "Value: ", es.String()))
	}
}

func printParserErrors(out io.Writer, errors []string) {
	for _, msg := range errors {
		io.WriteString(out, "\t"+msg+"\n")
	}
}
