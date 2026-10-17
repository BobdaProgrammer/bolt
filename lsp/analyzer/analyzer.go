package analyzer

import (
	"os"
	"strings"

	"reflect"

	"github.com/BobdaProgrammer/bolt/ast"
	"github.com/BobdaProgrammer/bolt/lexer"
	"github.com/BobdaProgrammer/bolt/lsp/logging"
	"github.com/BobdaProgrammer/bolt/parser"
	"github.com/BobdaProgrammer/bolt/std"
	"github.com/BobdaProgrammer/bolt/utils"
	"github.com/owenrumney/go-lsp/lsp"
	"path/filepath"
)

func CanonicalPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}

	return filepath.Clean(abs), nil
}

type Module struct {
	Name    string
	Exports []string
}

var Modulemap map[string]*Module = map[string]*Module{}

var builtins map[string]bool = map[string]bool{
	"len":    true,
	"push":   true,
	"delete": true,
	"panic":  true,
	"log":    true,
	"input":  true,
	"keys":   true,
	"typeof": true,
	"string": true,
	"int":    true,
	"float":  true,
	"copy":   true,
	"error":  true,
}

var ModuleDependencies map[string]map[string]bool = map[string]map[string]bool{}

func IsNil(v any) bool {
	if v == nil {
		return true
	}

	rv := reflect.ValueOf(v)

	switch rv.Kind() {
	case reflect.Ptr, reflect.Interface, reflect.Map,
		reflect.Slice, reflect.Func, reflect.Chan:
		return rv.IsNil()
	default:
		return false
	}
}

var RoleToInitType map[RoleType]InitType = map[RoleType]InitType{
	// ARGUMENT:             UNKNOWN,
	FUNCTION_NAME: FUNCTION,
	ARRAY_NAME:    ARRAY,
	ARRAY_INDEX:   INT,
	// INFIXED:              UNKNOWN,
	// ASSIGNING:            UNKNOWN,
	// ASSIGNER:             UNKNOWN,
	// HASH_KEY:             UNKNOWN,
	// HASH_VALUE:           UNKNOWN,
	STRUCT_TYPE:          STRUCT,
	STRUCT_INSTANCE_ROLE: STRUCT_INSTANCE,
}

const (
	_ InitType = iota
	INT
	FLOAT
	BOOL
	NULL
	STRING
	FUNCTION
	STRUCT
	STRUCT_INSTANCE
	STRUCT_CONVERT
	HASH
	ARRAY
	UNKNOWN
)

func getInitType(n ast.Node, scope *Scope) InitType {
	switch n := n.(type) {
	case *ast.FloatLiteral:
		return FLOAT
	case *ast.IntegerLiteral:
		return INT
	case *ast.Boolean:
		return BOOL
	case *ast.StringLiteral:
		return STRING
	case *ast.Null:
		return NULL
	case *ast.FunctionLiteral:
		return FUNCTION
	case *ast.StructType:
		return STRUCT
	case *ast.StructInstantiation:
		return STRUCT_INSTANCE
	case *ast.StructTypeConversion:
		return STRUCT_CONVERT
	case *ast.ArrayLiteral:
		return ARRAY
	case *ast.HashLiteral:
		return HASH
	case *ast.Identifier:
		if scope.Exists(n.Value) {
			return scope.Lookup(n.Value).Kind
		} else {
			return UNKNOWN
		}
	default:
		return UNKNOWN
	}
}

type InitType int

type Ident struct {
	Val      string
	Line     int
	UseType  RoleType
	NodeCopy ast.Node
}

type Initialised struct {
	Line     int
	Kind     InitType
	NodeCopy ast.Node
	Extras   map[string]int
}

type Scope struct {
	vals   map[string]*Initialised
	idents []Ident
	outer  *Scope
}

func (s *Scope) Exists(val string) bool {
	if _, ok := s.vals[val]; ok {
		return true
	} else if s.outer != nil {
		return s.outer.Exists(val)
	} else {
		return false
	}
}
func (s *Scope) Lookup(val string) *Initialised {
	if i, ok := s.vals[val]; ok {
		return i
	} else if s.outer != nil {
		return s.outer.Lookup(val)
	} else {
		return &Initialised{Line: -1, Kind: UNKNOWN}
	}
}

func NewScope() *Scope {
	return &Scope{
		vals:   map[string]*Initialised{},
		idents: []Ident{},
		outer:  nil,
	}
}

func NewEnclosedScope(outer *Scope) *Scope {
	return &Scope{
		vals:   map[string]*Initialised{},
		idents: []Ident{},
		outer:  outer,
	}
}

type LineDiagnostic struct {
	Line    int
	Message string
}

type Context struct {
	letIdent string
	inFor    bool
	lastNode ast.Node
	role     RoleType
}

type RoleType int

const (
	_ RoleType = iota
	ARGUMENT
	FUNCTION_NAME
	ARRAY_NAME
	ARRAY_INDEX
	INFIXED
	ASSIGNING
	HASH_NAME
	HASH_KEY
	HASH_VALUE
	ASSIGNER
	STRUCT_TYPE
	STRUCT_INSTANCE_ROLE
	RANGE_ROLE
	IF_CONDITION
	NIL
)

func GetImport(path string, line int) (*ast.Program, string, LineDiagnostic) {
	// If it doesn't end in .bolt then look for standard lib, otherwise read the file then parse
	var program *ast.Program = nil
	modulename := ""
	diags := LineDiagnostic{Line: -1, Message: ""}
	logging.Logger.Println("Getting import: ", path)
	if !strings.HasSuffix(path, ".bolt") && std.IsStandardLib(path) {
		program, _ = std.LoadStdPkg(path)
		modulename = path
	} else {
		if utils.FileExists(path) {
			fpath, _ := os.Stat(path)
			modulename = utils.FileName(fpath.Name())
			file := utils.ReadFile(path)
			program, _ = utils.ProcessModule(file, false)
		} else if line != -1 {
			diags = LineDiagnostic{Message: "Cannot find import", Line: line}
		}
	}

	return program, modulename, diags
}

func WalkImport(n ast.Node, scope *Scope, name string, isPub bool, exports *[]string) {
	// It isn't really walking since we are only really looking at top level statements for "pub"
	logging.Logger.Println("Walking import with name: ", name, "node: ", n.String())
	switch n := n.(type) {
	case *ast.ExpressionStatement:
		WalkImport(n.Expression, scope, name, false, exports)
	case *ast.BlockStatement:
		for _, stmt := range n.Statements {
			WalkImport(stmt, scope, name, false, exports)
		}

	case *ast.LetStatement:
		if i, ok := n.Left.(*ast.Identifier); ok && isPub {
			// TODO: Add data to initialised
			scope.vals[name+"."+i.Value] = &Initialised{}
			*exports = append(*exports, name+"."+i.Value)
			logging.Logger.Println("exporting: ", name+"."+i.Value)
		}
	case *ast.StructType:
		if isPub {
			*exports = append(*exports, n.Name.Value)
			// TODO: add data to initialised
			scope.vals[n.Name.Value] = &Initialised{}
		}

	case *ast.PubStatement:
		WalkImport(n.Stmt, scope, name, true, exports)
	}
}

func evalStructConversion(sc *ast.StructTypeConversion, diags *[]LineDiagnostic, scope *Scope) (*Initialised, map[string]ast.Expression) {
	if scope.Exists(sc.Left.Value) && scope.Exists(sc.StType.Value) {
		stInstance := scope.Lookup(sc.Left.Value)
		stType := scope.Lookup(sc.StType.Value)

		left := stInstance.NodeCopy.(*ast.StructInstantiation)
		right := stType.NodeCopy.(*ast.StructType)

		if len(left.Fields) > len(right.Fields) {
			logging.Logger.Println("Struct instance has more fields than struct type", sc.Line())
			*diags = append(*diags, LineDiagnostic{Message: "Cannot convert " + sc.Left.Value + " to " + sc.StType.Value + " since " + sc.Left.Value + " has more values than" + sc.StType.Value, Line: sc.Line()})
			logging.Logger.Println(LineDiagnostic{Message: "Cannot convert " + sc.Left.Value + " to " + sc.StType.Value + " since " + sc.Left.Value + " has more values than" + sc.StType.Value, Line: sc.Line()})
			return nil, nil
		}

		rightnames := map[string]bool{}
		for _, field := range right.Fields {
			rightnames[field.Value] = true
		}

		newMap := map[string]ast.Expression{}
		for name, val := range left.Fields {
			if _, ok := rightnames[name]; !ok {
				*diags = append(*diags, LineDiagnostic{Message: "Cannot convert " + sc.Left.Value + " to " + sc.StType.Value + " since " + sc.Left.Value + " has value " + name + " That is not a value of " + sc.StType.Value, Line: sc.Line()})
				return nil, nil
			} else {
				newMap[name] = val
			}
		}
		return stType, newMap
	} else {
		return nil, nil
	}
}

func walkInfix(n *ast.InfixExpression, scope *Scope, diags *[]LineDiagnostic) InitType {
	left := n.Left
	right := n.Right
	op := n.Operator

	var LeftType InitType
	var RightType InitType
	if ide, ok := left.(*ast.Identifier); ok && scope.Exists(ide.Value) {
		LeftType = scope.Lookup(ide.Value).Kind
	} else if inf, ok := left.(*ast.InfixExpression); ok {
		LeftType = walkInfix(inf, scope, diags)
	} else {
		LeftType = getInitType(left, scope)
	}
	if ide, ok := right.(*ast.Identifier); ok && scope.Exists(ide.Value) {
		RightType = scope.Lookup(ide.Value).Kind
	} else if inf, ok := right.(*ast.InfixExpression); ok {
		RightType = walkInfix(inf, scope, diags)
	} else {
		RightType = getInitType(right, scope)
	}

	if LeftType == UNKNOWN || RightType == UNKNOWN {
		return UNKNOWN
	}

	if op == "&&" || op == "||" {
		return BOOL
	}

	if LeftType != STRING && LeftType != BOOL && LeftType != INT && LeftType != FLOAT && LeftType != NULL && LeftType != UNKNOWN {
		*diags = append(*diags, LineDiagnostic{Message: "Cannot use " + left.String() + " of type " + InitTypeToString[LeftType] + " in infix expression", Line: left.Line()})
		return UNKNOWN
	}
	if RightType != STRING && RightType != BOOL && RightType != INT && RightType != FLOAT && RightType != NULL && RightType != UNKNOWN {
		*diags = append(*diags, LineDiagnostic{Message: "Cannot use " + right.String() + " of type " + InitTypeToString[RightType] + " in infix expression", Line: right.Line()})
		return UNKNOWN
	}

	var et InitType
	switch LeftType {
	case INT:
		if RightType != FLOAT && RightType != INT {
			if RightType == STRING && op != "*" {
				*diags = append(*diags, LineDiagnostic{Message: "Cannot use " + op + " Between INT and STRING", Line: n.Line()})
				return UNKNOWN
			} else {
				*diags = append(*diags, LineDiagnostic{Message: "Cannot use INT and " + InitTypeToString[RightType] + " In an infix expression", Line: n.Line()})
				return UNKNOWN
			}
		}

		if RightType == INT && op == "/" {
			et = FLOAT
		} else {
			et = RightType // set the expected type to the right type since int morphs to right e.g. int * int is int and int * float is float and int * string is string
		}
	case FLOAT:
		if RightType != FLOAT && RightType != INT {
			*diags = append(*diags, LineDiagnostic{Message: "Cannot use INT and " + InitTypeToString[RightType] + " In an infix expression", Line: n.Line()})
			return UNKNOWN
		}
		et = FLOAT
	case STRING:
		if RightType != INT && RightType != STRING {
			*diags = append(*diags, LineDiagnostic{Message: "Cannot use STRING and " + InitTypeToString[RightType] + " In an infix expression", Line: n.Line()})
			return UNKNOWN
		} else if RightType == INT && op != "*" {
			*diags = append(*diags, LineDiagnostic{Message: "Cannot use " + op + " Between STRING and INT", Line: n.Line()})
			return UNKNOWN
		} else if op != "==" && op != "!=" && op != "+" && op != "*" {
			*diags = append(*diags, LineDiagnostic{Message: "Cannot use " + op + " Between STRING and INT", Line: n.Line()})
			return UNKNOWN
		}
		et = STRING
	case BOOL:
		if RightType != BOOL {
			*diags = append(*diags, LineDiagnostic{Message: "Cannot use BOOL and " + InitTypeToString[RightType] + " In an infix expression", Line: n.Line()})
			return UNKNOWN
		} else if op != "==" && op != "&&" && op != "||" && op != "!=" {
			*diags = append(*diags, LineDiagnostic{Message: "Cannot use " + op + "BETWEEN BOOL AND BOOL", Line: n.Line()})
			return UNKNOWN
		}
		et = BOOL
	}

	if op == "==" || op == "!=" || op == "&&" || op == "||" {
		return BOOL
	}
	return et
}

func walk(n ast.Node, scope *Scope, scopes *[]*Scope, dependencies *map[string]bool, diags *[]LineDiagnostic, context Context) {
	// Context on what the variable name is (only used for struct initialisation)

	runWalk := func(node ast.Node, scope *Scope, rt RoleType, ctx ...Context) {
		if len(ctx) > 0 {
			walk(node, scope, scopes, dependencies, diags, ctx[0])
		} else {
			newCtx := context
			newCtx.lastNode = n
			newCtx.role = rt
			walk(node, scope, scopes, dependencies, diags, newCtx)
		}
	}

	// walk through the tree recursively. Any identifiers being accessed are recorded and any variables being initialised are also recorded
	switch n := n.(type) {
	case *ast.ExpressionStatement:
		runWalk(n.Expression, scope, NIL)
	case *ast.BlockStatement:
		for _, stmt := range n.Statements {
			runWalk(stmt, scope, NIL)
		}
	case *ast.BreakStatement:
		if !context.inFor {
			*diags = append(*diags, LineDiagnostic{Line: n.Line(), Message: "Cannot use break outside of loop"})
		}
	case *ast.ContinueStatement:
		if !context.inFor {
			*diags = append(*diags, LineDiagnostic{Line: n.Line(), Message: "Cannot use break outside of loop"})
		}
	case *ast.ArrayLiteral:
		for _, el := range n.Elements {
			runWalk(el, scope, NIL)
		}
	case *ast.ImportStatement:
		// if in top scope
		if scope.outer == nil {
			can, err := CanonicalPath(n.Value)
			path := n.Value
			if err == nil {
				path = can
			}
			logging.Logger.Println("Import path: ", path)

			if m, ok := Modulemap[can]; ok {
				for _, exp := range m.Exports {
					// TODO: add data to initialised
					scope.vals[exp] = &Initialised{}
				}
			} else {
				program, modulename, extradiags := GetImport(n.Value, n.Line())

				if extradiags.Line != -1 {
					*diags = append(*diags, extradiags)
				}

				// Show LSP error if import has errors
				if program != nil {
					exports := []string{}
					for _, stmt := range program.Statements {
						if !IsNil(stmt) {
							WalkImport(stmt, scope, modulename, false, &exports)
						}
					}
					Modulemap[path] = &Module{Name: path, Exports: exports}
					logging.Logger.Println("modulemap path: ", Modulemap[path])
				}
			}
			(*dependencies)[path] = true
		}

	case *ast.AssignExpression:
		runWalk(n.Left, scope, ASSIGNER)
		runWalk(n.Value, scope, ASSIGNING)
	case *ast.IndexExpression:
		if getInitType(n.Left, scope) == ARRAY {
			runWalk(n.Left, scope, ARRAY_NAME)
			if getInitType(n.Index, scope) != INT {
				*diags = append(*diags, LineDiagnostic{Message: "Cannot use " + n.Index.String() + " as index in index expression. Should be INT, got " + InitTypeToString[getInitType(n.Index, scope)], Line: n.Index.Line()})
			}
		} else if getInitType(n.Left, scope) == HASH {
			runWalk(n.Left, scope, HASH_NAME)
			indType := getInitType(n.Index, scope)
			if indType != STRING && indType != INT && indType != BOOL && indType != ARRAY && indType != HASH && indType != FLOAT {
				*diags = append(*diags, LineDiagnostic{Message: "Cannot use " + n.Index.String() + " as index in hash index expression. got " + InitTypeToString[getInitType(n.Index, scope)], Line: n.Index.Line()})
			}
		}
	case *ast.IndexAssignExpression:

		if getInitType(n.Left, scope) == ARRAY {
			runWalk(n.Left, scope, ARRAY_NAME)
			if getInitType(n.Index, scope) != INT {
				*diags = append(*diags, LineDiagnostic{Message: "Cannot use " + n.Index.String() + " as index in index expression. Should be INT, got " + InitTypeToString[getInitType(n.Index, scope)], Line: n.Index.Line()})
			}
		} else if getInitType(n.Left, scope) == HASH {
			runWalk(n.Left, scope, HASH_NAME)
			indType := getInitType(n.Index, scope)
			if indType != STRING && indType != INT && indType != BOOL && indType != ARRAY && indType != HASH && indType != FLOAT {
				*diags = append(*diags, LineDiagnostic{Message: "Cannot use " + n.Index.String() + " as index in hash index expression. got " + InitTypeToString[getInitType(n.Index, scope)], Line: n.Index.Line()})
			}
		}
		runWalk(n.Value, scope, ASSIGNING)
	case *ast.SliceExpression:
		runWalk(n.Left, scope, ARRAY_NAME)
		runWalk(n.IndexStart, scope, ARRAY_INDEX)
		runWalk(n.IndexEnd, scope, ARRAY_INDEX)
	case *ast.HashLiteral:
		for k, v := range n.Pairs {
			runWalk(k, scope, HASH_KEY)
			runWalk(v, scope, HASH_VALUE)
		}
	case *ast.StructTypeConversion:
		runWalk(n.StType, scope, STRUCT_TYPE)
		runWalk(n.Left, scope, STRUCT_INSTANCE_ROLE)
		if _, ok := context.lastNode.(*ast.FieldAccess); !ok {
			evalStructConversion(n, diags, scope)
		}
	case *ast.RangeExpression:
		runWalk(n.Ranging, scope, RANGE_ROLE)
		t := getInitType(n.Ranging, scope)
		if id, ok := n.Ranging.(*ast.Identifier); ok && scope.Exists(id.Value) {
			t = scope.Lookup(id.Value).Kind
		}
		if t != INT && t != FLOAT && t != ARRAY && t != HASH && t != STRING {
			*diags = append(*diags, LineDiagnostic{Message: "Cannot range over " + InitTypeToString[t], Line: n.Ranging.Line()})
		}
	case *ast.Defer:
		runWalk(n.Exp, scope, NIL)
	case *ast.LetStatement:
		// We get the identifier on the left hand side
		identstr := ""
		if ident, ok := n.Left.(*ast.Identifier); ok {
			scope.vals[ident.Value] = &Initialised{
				Line: n.Left.Line(),
				Kind: getInitType(n.Value, scope),
			}
			identstr = ident.Value
		} else if fa, ok := n.Left.(*ast.FieldAccess); ok {
			leftval := scope.Lookup(fa.Left.String())
			if scope.Exists(fa.Left.String()) && leftval.Kind == STRUCT {
				if fn, ok := n.Value.(*ast.FunctionLiteral); ok {
					// create a new enclosed scope and walk the function
					newscope := NewEnclosedScope(scope)
					*scopes = append(*scopes, newscope)
					for _, param := range fn.Parameters {
						newscope.vals[param.Value] = &Initialised{
							Line: n.Line(),
							Kind: UNKNOWN,
						}
					}
					newscope.vals[fa.Left.String()] = &Initialised{Line: leftval.Line, Kind: UNKNOWN}
					str := leftval.NodeCopy.(*ast.StructType)

					leftval.Extras[fa.Right.String()] = fa.Line()

					for _, field := range str.Fields {
						newscope.vals[fa.Left.String()+"."+field.Value] = &Initialised{Line: leftval.Line, Kind: UNKNOWN}
					}
					runWalk(fn.Body, newscope, NIL)
					break
				}
			}
		}

		// If it is a struct instantiation we want to be adding different initialisations for <variable-name>.<struct-field>
		if si, ok := n.Value.(*ast.StructInstantiation); ok {
			if identstr != "" {
				// TODO: do this for the other struct instantiation code below
				scope.vals[identstr] = &Initialised{Line: n.Left.Line(), Kind: getInitType(n.Value, scope), NodeCopy: si}
				if id, ok := si.Left.(*ast.Identifier); ok {
					if scope.Exists(id.Value) {
						stru := scope.Lookup(id.Value)
						if stru.Kind != STRUCT {
							*diags = append(*diags, LineDiagnostic{Message: "Cannot use struct instantiation with non struct " + id.Value + " of type " + InitTypeToString[stru.Kind], Line: si.Left.Line()})
							break
						}

						fieldmap := map[string]bool{}
						st := stru.NodeCopy.(*ast.StructType)
						for _, field := range st.Fields {
							fieldmap[field.Value] = true
						}

						for method, line := range stru.Extras {
							scope.vals[identstr+"."+method] = &Initialised{
								Line: line,
								Kind: FUNCTION,
							}
						}

						for name, val := range si.Fields {
							if _, ok := fieldmap[name]; !ok {
								*diags = append(*diags, LineDiagnostic{Message: "Struct type " + id.Value + " has no field " + name, Line: n.Value.Line()})
								break
							}
							scope.vals[identstr+"."+name] = &Initialised{
								Line: n.Left.Line(),
								Kind: getInitType(val, scope),
							}
							// Walk recursively to search for other identifiers
							runWalk(val, scope, NIL, Context{letIdent: identstr + "." + name, inFor: context.inFor, lastNode: si})
						}
					}
				} else {
					*diags = append(*diags, LineDiagnostic{Message: "Cannot use " + si.Left.String() + "As struct in struct instantiation", Line: si.Left.Line()})
				}
			}
		} else if sc, ok := n.Value.(*ast.StructTypeConversion); ok {
			st, fields := evalStructConversion(sc, diags, scope)
			if st != nil && len(fields) > 0 {
				sifields := map[string]ast.Expression{}
				si := &ast.StructInstantiation{LineNum: n.Left.Line()}
				for ex, l := range st.Extras {
					scope.vals[identstr+"."+ex] = &Initialised{
						Line: l,
						Kind: FUNCTION,
					}
				}
				stType := st.NodeCopy.(*ast.StructType)
				si.Left = &ast.Identifier{Value: stType.Name.Value}

				for name, t := range fields {
					scope.vals[identstr+"."+name] = &Initialised{
						Line: sc.Line(),
						Kind: getInitType(t, scope),
					}
					sifields[name] = t
				}
				si.Fields = sifields
				scope.vals[identstr] = &Initialised{Line: n.Left.Line(), Kind: STRUCT_INSTANCE, NodeCopy: si}
			}
		} else {
			// Walk the other side of the let statement
			runWalk(n.Value, scope, ASSIGNING)
			if id, ok := n.Value.(*ast.Identifier); ok && scope.Exists(id.Value) {
				t := scope.Lookup(id.Value)
				scope.vals[identstr] = &Initialised{
					Line: n.Left.Line(),
					Kind: t.Kind,
				}
				if t.Kind == STRUCT_INSTANCE {
					si := t.NodeCopy.(*ast.StructInstantiation)
					left, ok := si.Left.(*ast.Identifier)
					if ok && scope.Exists(left.Value) {
						stType := scope.Lookup(left.Value)
						if stType.Kind == STRUCT {
							for ext, l := range stType.Extras {
								scope.vals[identstr+"."+ext] = &Initialised{
									Line: l,
									Kind: FUNCTION,
								}
							}
						}
						for field, v := range si.Fields {
							scope.vals[identstr+"."+field] = &Initialised{
								Line: si.Line(),
								Kind: getInitType(v, scope),
							}
						}
					}
				}
			}
		}
	case *ast.StructInstantiation:
		if id, ok := n.Left.(*ast.Identifier); ok {
			if scope.Exists(id.Value) {
				stru := scope.Lookup(id.Value)
				if stru.Kind != STRUCT {
					*diags = append(*diags, LineDiagnostic{Message: "Cannot use struct instantiation with non struct " + id.Value + " of type " + InitTypeToString[stru.Kind], Line: n.Left.Line()})
					break
				}

				fieldmap := map[string]bool{}
				st := stru.NodeCopy.(*ast.StructType)
				for _, field := range st.Fields {
					fieldmap[field.Value] = true
				}

				if context.letIdent != "" {
					for method, line := range stru.Extras {
						scope.vals[context.letIdent+"."+method] = &Initialised{
							Line: line,
							Kind: FUNCTION,
						}
					}
				}

				for name, val := range n.Fields {
					if _, ok := fieldmap[name]; !ok {
						*diags = append(*diags, LineDiagnostic{Message: "Struct type " + id.Value + " has no field " + name, Line: n.Line()})
						break
					}
					if context.letIdent != "" {
						scope.vals[context.letIdent+"."+name] = &Initialised{
							Line: n.Left.Line(),
							Kind: getInitType(val, scope),
						}
						// Walk recurnvely to search for other identifiers
						runWalk(val, scope, NIL, Context{letIdent: context.letIdent + "." + name, inFor: context.inFor, lastNode: n})
					} else {
						runWalk(val, scope, NIL)
					}
				}
			}
		}

		// // Go though each field and if we have context (meaning we are in a let statement) then assign an identifier to it, then walk each val
		// for name, val := range n.Fields {
		// 	if context.letIdent != "" {
		// 		scope.vals[context.letIdent+"."+name] = &Initialised{
		// 			Line: n.Left.Line(),
		// 			Kind: getInitType(val, scope),
		// 		}
		// 		runWalk(val, scope, NIL, Context{letIdent: context.letIdent + "." + name, inFor: context.inFor, lastNode: n})
		// 	} else {
		// 		runWalk(val, scope, NIL)
		// 	}
		// }
	case *ast.StructType:
		// record struct type initialisation
		scope.vals[n.Name.Value] = &Initialised{
			Line:     n.Line(),
			Kind:     getInitType(n, scope),
			NodeCopy: n,
			Extras:   map[string]int{},
		}

	case *ast.FunctionLiteral:
		// create a new enclosed scope and walk the function
		newscope := NewEnclosedScope(scope)
		*scopes = append(*scopes, newscope)
		for _, param := range n.Parameters {
			newscope.vals[param.Value] = &Initialised{
				Line: n.Line(),
				Kind: UNKNOWN,
			}
		}
		runWalk(n.Body, newscope, NIL)
	case *ast.IfExpression:
		// create a new enclosed scope and walk the if statement and its alternatives
		runWalk(n.Condition, scope, IF_CONDITION)
		newscope := NewEnclosedScope(scope)
		*scopes = append(*scopes, newscope)
		runWalk(n.Consequence, newscope, NIL)
		if n.Alternative != nil {
			runWalk(n.Alternative, scope, NIL)
		}
	case *ast.ForExpression:
		// create a new enclosed scope and walk the for loop
		runWalk(n.Condition, scope, NIL)
		newscope := NewEnclosedScope(scope)
		*scopes = append(*scopes, newscope)
		runWalk(n.Consequence, newscope, NIL, Context{letIdent: "", inFor: true, lastNode: n})
	case *ast.SwitchExpression:
		// Walk the switch item then each case
		runWalk(n.SwitchItem, scope, NIL)
		for _, cas := range n.Cases {
			// walk the case condition and also the body
			runWalk(cas.Condition, scope, NIL)
			newscope := NewEnclosedScope(scope)
			*scopes = append(*scopes, newscope)
			runWalk(cas.Consequence, newscope, NIL)
		}
	case *ast.ReturnStatement:
		// walk each return value
		for _, val := range n.ReturnValues {
			runWalk(val, scope, NIL)
		}
	case *ast.PubStatement:
		// walk whats after the pub
		runWalk(n.Stmt, scope, NIL)
	case *ast.InfixExpression:
		// walk both sides of infix expression
		runWalk(n.Left, scope, INFIXED)
		runWalk(n.Right, scope, INFIXED)
		// init := scope.Lookup(ident.Val)
		// if init.Kind != STRING && init.Kind != BOOL && init.Kind != INT && init.Kind != FLOAT && init.Kind != NULL && init.Kind != UNKNOWN {
		// 	*diags = append(*diags, LineDiagnostic{Message: "Cannot use " + ident.Val + " of type " + InitTypeToString[init.Kind] + " in infix expression", Line: ident.Line})
		// }
		//
		//infix := ident.NodeCopy.(*ast.InfixExpression)
		walkInfix(n, scope, diags)
	case *ast.CallExpression:
		// walk the function and its arguments
		runWalk(n.Function, scope, FUNCTION_NAME)
		for _, arg := range n.Arguments {
			runWalk(arg, scope, ARGUMENT)
		}
	case *ast.Identifier:
		// record identifier use
		i := Ident{Val: n.Value, Line: n.Line(), UseType: context.role}
		if context.role == INFIXED {
			i.NodeCopy = context.lastNode
		}
		scope.idents = append(scope.idents, i)
	case *ast.FieldAccess:
		if i, ok := n.Left.(*ast.Identifier); ok {
			// record field access use as identifier
			i := fieldAccess(n, context, scope, diags, i.Value) //Ident{Val: n.String(), Line: n.Line(), UseType: context.role}
			if context.role == INFIXED {
				i.NodeCopy = context.lastNode
			}
			scope.idents = append(scope.idents, i)
		} else {
			runWalk(n.Left, scope, NIL)
			if sc, ok := n.Left.(*ast.StructTypeConversion); ok {
				if id, ok := n.Right.(*ast.Identifier); ok {
					logging.Logger.Println("eval struct conv")
					st, fields := evalStructConversion(sc, diags, scope)
					logging.Logger.Println("evalled struct conv", st == nil, len(fields))
					if len(fields) != 0 && st != nil {
						_, isfield := fields[id.Value]
						_, ismethod := st.Extras[id.Value]
						if !isfield && !ismethod {
							*diags = append(*diags, LineDiagnostic{Message: id.Value + " is not a value of STRUCT INSTANCE " + sc.Left.Value + " that has been converted to " + sc.StType.Value, Line: n.Right.Line()})
						}
					}
				} else {
					*diags = append(*diags, LineDiagnostic{Message: "Cannot use " + n.Right.String() + " on STRUCT INSTANCE", Line: n.Right.Line()})
				}
			}
		}
	}

}

// Give: hello.bob.jeff
// It returns and ident of hello.bob.jeff
// Give: hello.bob[2]
// it returns ident of hello.bob with use of ARRAY
func fieldAccess(fa *ast.FieldAccess, context Context, scope *Scope, diags *[]LineDiagnostic, curr string) Ident {
	if id, ok := fa.Right.(*ast.Identifier); ok {
		curr += "." + id.Value
		return Ident{
			Val:     curr,
			Line:    fa.Line(),
			UseType: context.role,
		}
	} else if f, ok := fa.Right.(*ast.FieldAccess); ok {
		if id, ok := f.Left.(*ast.Identifier); ok {
			curr += "." + id.Value
			return fieldAccess(f, context, scope, diags, curr)
		} else {
			*diags = append(*diags, LineDiagnostic{Message: "Cannot use " + f.Left.String() + " In field access", Line: f.Left.Line()})
			return Ident{}
		}
	} else if ie, ok := fa.Right.(*ast.IndexExpression); ok {

		if id, ok := ie.Left.(*ast.Identifier); ok {
			curr += "." + id.Value
			exists := scope.Exists(curr)
			ut := ARRAY_NAME
			if exists {
				val := scope.Lookup(curr)
				if val.Kind == ARRAY && getInitType(ie.Index, scope) != INT {
					*diags = append(*diags, LineDiagnostic{Message: "Cannot use " + ie.Index.String() + " as index in index expression. Should be INT, got " + InitTypeToString[getInitType(ie.Index, scope)], Line: ie.Index.Line()})
				} else if val.Kind == HASH {
					indType := getInitType(ie.Index, scope)
					if indType != STRING && indType != INT && indType != BOOL && indType != ARRAY && indType != HASH && indType != FLOAT {
						*diags = append(*diags, LineDiagnostic{Message: "Cannot use " + ie.Index.String() + " as index in hash index expression. got " + InitTypeToString[getInitType(ie.Index, scope)], Line: ie.Index.Line()})
					}
				}
				if val.Kind == HASH {
					ut = HASH_NAME
				}
			}
			return Ident{
				Val:     curr,
				Line:    fa.Line(),
				UseType: ut,
			}
		} else {
			*diags = append(*diags, LineDiagnostic{Message: "Cannot use " + ie.Left.String() + " In field access", Line: ie.Left.Line()})
			return Ident{}
		}
	} else {
		*diags = append(*diags, LineDiagnostic{Message: "Cannot use " + ie.Left.String() + " In field access", Line: ie.Left.Line()})
		return Ident{}
	}
}

func diag(Message string, line int, lines []string, sev int) lsp.Diagnostic {
	if line < 1 {
		return lsp.Diagnostic{}
	}
	seve := lsp.SeverityError
	if sev == 1 {
		seve = lsp.SeverityWarning
	}

	return lsp.Diagnostic{
		Range: lsp.Range{
			Start: lsp.Position{
				Line:      line - 1,
				Character: 0,
			},
			End: lsp.Position{
				Line:      line - 1,
				Character: len(lines[line-1]),
			},
		},

		Severity: &seve,
		Source:   "bolt",
		Message:  Message,
	}
}

func CheckInitsAndIdents(program *ast.Program, lines []string, path string) []lsp.Diagnostic {
	dependencies := map[string]bool{}
	linediags := []LineDiagnostic{}
	mainScope := NewScope()
	scopes := []*Scope{}
	for _, stmt := range program.Statements {
		if !IsNil(stmt) {
			walk(stmt, mainScope, &scopes, &dependencies, &linediags, Context{inFor: false, letIdent: "", lastNode: nil})
		}
	}
	ModuleDependencies[path] = dependencies

	diags := []lsp.Diagnostic{}
	for _, d := range linediags {
		diags = append(diags, diag(d.Message, d.Line, lines, 0))
	}
	scopes = append(scopes, mainScope)
	logging.Logger.Println("Scopes", len(scopes))
	for _, scope := range scopes {
		for _, init := range scope.vals {
			if init.Line == -1 || init.Kind == 0 {
				continue
			}

		}
		for _, ident := range scope.idents {
			if _, ok := builtins[ident.Val]; !ok && !scope.Exists(ident.Val) {
				diags = append(diags, diag("Identifier not found: "+ident.Val, ident.Line, lines, 0))
			} else if _, ok := builtins[ident.Val]; ok {
				if ident.UseType != FUNCTION_NAME {
					diags = append(diags, diag("Cannot use builtin: "+ident.Val+" as non-function", ident.Line, lines, 0))
				}
			} else if init, ok := RoleToInitType[ident.UseType]; ok {
				it := scope.Lookup(ident.Val)
				if it.Kind != init && init != UNKNOWN && it.Kind != UNKNOWN {
					diags = append(diags, diag("Cannot use "+ident.Val+" which is "+InitTypeToString[it.Kind]+" as "+InitTypeToString[init], ident.Line, lines, 0))
				}
			}

		}
	}

	return diags
}

var InitTypeToString map[InitType]string = map[InitType]string{
	INT:             "INT",
	FLOAT:           "FLOAT",
	BOOL:            "BOOL",
	STRING:          "STRING",
	FUNCTION:        "FUNCTION",
	STRUCT:          "STRUCT",
	STRUCT_INSTANCE: "STRUCT INSTANCE",
	ARRAY:           "ARRAY",
	HASH:            "HASH",
	UNKNOWN:         "UNKNOWN",
}

func Parse(source string) (*ast.Program, []lsp.Diagnostic) {
	l := lexer.New(source)
	p := parser.New(l)
	program := p.ParseProgram()
	lines := strings.Split(source, "\n")

	if len(p.Errors()) != 0 {
		errs := p.ParserErrors()
		sev := lsp.SeverityError
		diags := []lsp.Diagnostic{}
		for _, e := range errs {
			diags = append(diags, lsp.Diagnostic{
				Range: lsp.Range{
					Start: lsp.Position{
						Line:      e.Line - 1,
						Character: 0,
					},
					End: lsp.Position{
						Line:      e.Line - 1,
						Character: len(lines[e.Line-1]),
					},
				},
				Severity: &sev,
				Source:   "bolt",
				Message:  e.Message,
			})
		}
		return program, diags
	}

	return program, []lsp.Diagnostic{}
}
