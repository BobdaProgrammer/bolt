package hover

import (
	"strings"

	"github.com/BobdaProgrammer/bolt/ast"
	"github.com/BobdaProgrammer/bolt/lexer"
	"github.com/BobdaProgrammer/bolt/lsp/analyzer"
	"github.com/BobdaProgrammer/bolt/lsp/logging"
)

func Documentation(n ast.Node, init *analyzer.Initialised, letIdent string, scope *analyzer.Scope, comments map[int]lexer.Comment) string {
	doc := ""
	switch n := n.(type) {
	case *ast.FunctionLiteral:
		doc = "Type: FUNCTION\n\n```bolt\n" + letIdent + " = fn("
		params := []string{}
		for _, param := range n.Parameters {
			params = append(params, param.Value)
		}
		doc += strings.Join(params, ", ") + ")\n```"
		if comment, ok := comments[init.Line-1]; ok {
			doc += "\n" + comment.Message
		}
	case *ast.StructInstantiation:
		doc = "Type: STRUCT_INSTANCE\nStruct Type: "
		doc += "`" + n.Left.String() + "`\n\n"
		fields := ""
		codestr := letIdent + " = new " + n.Left.String() + "{"
		i := 0
		for name, val := range n.Fields {
			codestr += name.Value + ": ..."
			if i != len(n.Fields)-1 {
				codestr += ", "
			}
			fields += "   - `" + name.Value + "`"
			if analyzer.GetInitType(val, scope) != analyzer.UNKNOWN {
				fields += ": " + analyzer.InitTypeToString[analyzer.GetInitType(val, scope)] + "\n"
			} else {
				fields += "\n"
			}
			i++
		}
		codestr += "}"
		doc += "```bolt\n" + codestr + "\n```\n\n"
		doc += "Fields:\n" + strings.TrimRight(fields, "\n")
		if scope.Exists(n.Left.String()) {
			stInit := scope.Lookup(n.Left.String())
			if stInit.Kind != analyzer.STRUCT || stInit.NodeCopy == nil {
				goto structTypeEnd
			}

			st, ok := stInit.NodeCopy.(*ast.StructType)
			if !ok {
				goto structTypeEnd
			}

			if len(st.Fields) > 0 {
				doc += "\n\nAvailable struct fields of `" + n.Left.String() + "`:\n"
				stTypeFields := ""
				for _, field := range st.Fields {
					stTypeFields += "   - `" + field.Value + "`\n"
				}
				doc += strings.TrimRight(stTypeFields, "\n")
			}

			if len(stInit.Extras) > 0 {
				doc += "\n\nAvailable struct methods of `" + n.Left.String() + "`:\n"
				stMethodFields := ""
				for method := range stInit.Extras {
					stMethodFields += "   - `" + method + "()`\n"
				}
				doc += strings.TrimRight(stMethodFields, "\n")
			}

		}
	structTypeEnd:
		logging.Logger.Println("Hover struct instance: ", doc)
	case *ast.StructType:
		doc = "Type: STRUCT\n\n```bolt\n" + n.String() + "\n```"
		if comment, ok := comments[init.Line-1]; ok {
			doc += "\n" + comment.Message
		}
	}

	return doc
}
