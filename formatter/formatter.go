package formatter

import "bolt/ast"

var out string = ""

func Format(ast *ast.Program) string {
	for _, stmt := range ast.Statements {
		out += stmt.String() + "\n"
	}

	return out
}
