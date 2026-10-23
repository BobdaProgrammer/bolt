package analyzer

import (
	"math"
	"os"
	"strings"

	"reflect"

	"path/filepath"

	"github.com/BobdaProgrammer/bolt/ast"
	"github.com/BobdaProgrammer/bolt/lexer"
	"github.com/BobdaProgrammer/bolt/lsp/logging"
	"github.com/BobdaProgrammer/bolt/parser"
	"github.com/BobdaProgrammer/bolt/std"
	"github.com/BobdaProgrammer/bolt/token"
	"github.com/BobdaProgrammer/bolt/utils"
	"github.com/owenrumney/go-lsp/lsp"
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
	Exports []Export
}

type Position struct {
	Line, StartColumn, EndColumn int
}

type LocationInit struct {
	Scope *Scope
	Name  string
	Init  *Initialised
	Pos   Position
}

type DocumentIdentLines struct {
	Idents []LocationInit
}

var DocToIdentLines map[lsp.DocumentURI]DocumentIdentLines = map[lsp.DocumentURI]DocumentIdentLines{}

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
	MODULE
	UNKNOWN
)

func GetInitType(n ast.Node, scope *Scope) InitType {
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
	case *ast.FieldAccess:
		if scope.Exists(n.String()) {
			return scope.Lookup(n.String()).Kind
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
	Token    token.Token
	UseType  RoleType
	NodeCopy ast.Node
}

type Initialised struct {
	Line       int
	Column     int
	Kind       InitType
	NodeCopy   ast.Node
	ImportFile string              // only for imported vals
	Extras     map[string]Position // TODO: give a node copy
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
		return &Initialised{Line: -1, Column: -1, Kind: UNKNOWN}
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
	Token   token.Token
}

type Context struct {
	letIdent string
	inFor    bool
	isStd    bool
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

func GetImport(path string, line int) (*ast.Program, string, LineDiagnostic, bool, string) {
	// If it doesn't end in .bolt then look for standard lib, otherwise read the file then parse
	var program *ast.Program = nil
	modulename := ""
	diags := LineDiagnostic{Line: -1, Message: ""}
	logging.Logger.Println("Getting import: ", path)
	dest := ""
	isStd := false
	if !strings.HasSuffix(path, ".bolt") && std.IsStandardLib(path) {
		dest, _ = std.ExtractFile(path)
		program, _ = std.LoadStdPkg(path)
		modulename = path
		isStd = true
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

	return program, modulename, diags, isStd, dest
}

type Export struct {
	Name string
	Init *Initialised
}

func getStartColumn(tok token.Token) int {
	return tok.Column - (len(tok.Literal) - 1)
}

func walkStructInstantiations(context Context, si *ast.StructInstantiation, curr string, scope *Scope, diags *[]LineDiagnostic, runWalk func(n ast.Node, r RoleType, c ...Context), pos Position, init *Initialised) {

	scope.vals[curr] = &Initialised{Line: pos.Line, Column: pos.StartColumn, Kind: STRUCT_INSTANCE, NodeCopy: si, Extras: map[string]Position{}}
	if id, ok := si.Left.(*ast.Identifier); ok {
		runWalk(id, STRUCT_TYPE)
		fmap := map[string]bool{}
		foundStruct := false
		if scope.Exists(id.Value) {
			foundStruct = true
			stru := scope.Lookup(id.Value)
			if stru.Kind != STRUCT {
				*diags = append(*diags, LineDiagnostic{Message: "Cannot use struct instantiation with non struct " + id.Value + " of type " + InitTypeToString[stru.Kind], Line: si.Left.Line()})
				return
			}

			fieldmap := map[string]bool{}
			st := stru.NodeCopy.(*ast.StructType)
			for _, field := range st.Fields {
				fieldmap[field.Value] = true
				scope.vals[curr+"."+field.Value] = &Initialised{
					Line:   field.Line(),
					Column: getStartColumn(field.Token),
					Kind:   UNKNOWN,
				}
			}

			for method, pos := range stru.Extras {
				scope.vals[curr].Extras[method] = Position{Line: pos.Line, StartColumn: pos.StartColumn, EndColumn: pos.EndColumn}
				scope.vals[curr+"."+method] = &Initialised{
					Line:   pos.Line,
					Column: pos.StartColumn,
					Kind:   FUNCTION,
				}
			}

			fmap = fieldmap
		} else if init != nil && init.Extras != nil && len(init.Extras) > 0 {
			for method, pos := range init.Extras {
				scope.vals[curr].Extras[method] = Position{Line: pos.Line, StartColumn: pos.StartColumn, EndColumn: pos.EndColumn}
				scope.vals[curr+"."+method] = &Initialised{
					Line:   pos.Line,
					Column: pos.StartColumn,
					Kind:   FUNCTION,
				}
			}
		}
		for name, val := range si.Fields {
			if foundStruct {
				if _, ok := fmap[name]; !ok {
					*diags = append(*diags, LineDiagnostic{Message: "Struct type " + id.Value + " has no field " + name, Line: si.Line()})
					break
				}
			}

			tok, fine := getToken(val)
			var col int
			if fine {
				col = getStartColumn(tok)
			} else {
				col = 1
			}

			scope.vals[curr+"."+name] = &Initialised{
				Line:     val.Line(),
				Column:   col,
				NodeCopy: val,
				Kind:     GetInitType(val, scope),
			}
			// Walk recursively to search for other identifiers
			runWalk(val, NIL, Context{letIdent: curr + "." + name, inFor: context.inFor, lastNode: si})
		}
	} else {
		*diags = append(*diags, LineDiagnostic{Message: "Cannot use " + si.Left.String() + "As struct in struct instantiation", Line: si.Left.Line()})
	}

}

func walkStructInstantiationsImport(val ast.Expression, curr string, scope *Scope, privScope *Scope, imp string) {
	// TODO:remove logs
	if sInst, ok := val.(*ast.StructInstantiation); ok {
		scope.vals[curr] = &Initialised{Line: val.Line(), Column: getStartColumn(sInst.Token), Kind: STRUCT_INSTANCE, NodeCopy: sInst, ImportFile: imp, Extras: map[string]Position{}}
		if stTypeIdent, ok := sInst.Left.(*ast.Identifier); ok {
			if !privScope.Exists(stTypeIdent.Value) {
				return
			}

			stType := privScope.Lookup(stTypeIdent.Value)
			for method, position := range stType.Extras {
				scope.vals[curr].Extras[method] = Position{Line: position.Line, StartColumn: position.StartColumn, EndColumn: position.EndColumn}
				scope.vals[curr+"."+method] = &Initialised{
					Line:       position.Line,
					Column:     position.StartColumn,
					Kind:       FUNCTION,
					ImportFile: imp,
				}
			}
			for field, value := range sInst.Fields {
				tok, fine := getToken(value)
				col := 1
				if fine {
					col = getStartColumn(tok)
				}
				scope.vals[curr+"."+field] = &Initialised{
					Line:       val.Line(),
					Column:     col,
					Kind:       GetInitType(value, scope),
					ImportFile: imp,
					NodeCopy:   value,
				}

				if GetInitType(value, scope) == STRUCT_INSTANCE {
					walkStructInstantiationsImport(value, curr+"."+field, scope, privScope, imp)
				}
			}
		}
	}
}

func ProcessImport(p *ast.Program, modulename string, imp string) []Export {
	privScope := NewScope()
	sc := NewScope()
	for _, stmt := range p.Statements {
		if !IsNil(stmt) {
			WalkImport(stmt, sc, privScope, modulename, false, imp)
		}
	}

	exports := []Export{Export{Name: modulename, Init: &Initialised{Line: 1, Column: 1, ImportFile: imp, Kind: MODULE}}}
	for letident, init := range sc.vals {
		if init.Kind == STRUCT_INSTANCE {
			logging.Logger.Println("Import: ", letident)
			if init.NodeCopy == nil {
				continue
			}
			si, ok := init.NodeCopy.(*ast.StructInstantiation)
			if !ok {
				logging.Logger.Println("Could not convert", letident, "with init", init.NodeCopy, "to struct instance in import")
				continue
			}

			if privScope.Exists(si.Left.String()) {
				stType := privScope.Lookup(si.Left.String())
				for method, pos := range stType.Extras {
					init.Extras[method] = Position{Line: pos.Line, StartColumn: pos.StartColumn, EndColumn: pos.EndColumn}
					exports = append(exports, Export{Name: letident + "." + method, Init: &Initialised{
						Line:       pos.Line,
						Column:     pos.StartColumn,
						Kind:       FUNCTION,
						ImportFile: imp,
					}})
				}
			}
		}
		exports = append(exports, Export{Name: letident, Init: init})
	}
	return exports
}

func WalkImport(n ast.Node, scope *Scope, privScope *Scope, name string, isPub bool, imp string) {
	// It isn't really walking since we are only really looking at top level statements for "pub"
	switch n := n.(type) {
	case *ast.ExpressionStatement:
		WalkImport(n.Expression, scope, privScope, name, false, imp)
	case *ast.BlockStatement:
		for _, stmt := range n.Statements {
			WalkImport(stmt, scope, privScope, name, false, imp)
		}

	case *ast.LetStatement:
		var letstr string = ""
		if i, ok := n.Left.(*ast.Identifier); ok && isPub {
			letstr = i.Value
			in := &Initialised{Line: n.Left.Line(), Column: getStartColumn(i.Token), Kind: GetInitType(n.Value, scope), ImportFile: imp, NodeCopy: n.Value}
			scope.vals[name+"."+i.Value] = in
		} else if fa, ok := n.Left.(*ast.FieldAccess); ok && privScope.Exists(fa.Left.String()) {

			// use privscope here to avoid having to do name+"."
			if privScope.Lookup(fa.Left.String()).Kind != STRUCT {
				break
			}

			if id, ok := fa.Right.(*ast.Identifier); ok {
				privScope.Lookup(fa.Left.String()).Extras[id.Value] = Position{Line: id.Line(), StartColumn: getStartColumn(id.Token), EndColumn: id.Token.Column}
			}
		} else if arr, ok := n.Left.(*ast.ArrayLiteral); ok && isPub {
			for _, el := range arr.Elements {
				if id, ok := el.(*ast.Identifier); ok {
					scope.vals[name+"."+id.Value] = &Initialised{
						Line:       id.Line(),
						Column:     getStartColumn(id.Token),
						ImportFile: imp,
						Kind:       UNKNOWN,
					}
				} else if fa, ok := el.(*ast.FieldAccess); ok {
					scope.vals[name+"."+fieldAccessStr(fa, fa.Left.String())] = &Initialised{
						Line:       fa.Line(),
						Column:     getStartColumn(fa.Token),
						ImportFile: imp,
						Kind:       UNKNOWN,
					}
				}
			}
		}

		// TODO: deal with saying let x = math where math is a struct instance
		if si, ok := n.Value.(*ast.StructInstantiation); ok && isPub && letstr != "" {
			walkStructInstantiationsImport(si, name+"."+letstr, scope, privScope, imp)
		}
	case *ast.StructType:
		if isPub {
			i := &Initialised{Line: n.Name.Line(), Column: getStartColumn(n.Name.Token), Kind: STRUCT, ImportFile: imp, Extras: map[string]Position{}, NodeCopy: n}
			scope.vals[name+"."+n.Name.Value] = i
		}
		i := &Initialised{Line: n.Name.Line(), Column: getStartColumn(n.Name.Token), Kind: STRUCT, Extras: map[string]Position{}}

		privScope.vals[n.Name.Value] = i

	case *ast.PubStatement:
		WalkImport(n.Stmt, scope, privScope, name, true, imp)
	}
}

func evalStructConversion(sc *ast.StructTypeConversion, diags *[]LineDiagnostic, scope *Scope) (*Initialised, map[string]ast.Expression) {
	if scope.Exists(sc.Left.Value) && scope.Exists(sc.StType.Value) {
		stInstance := scope.Lookup(sc.Left.Value)
		stType := scope.Lookup(sc.StType.Value)

		left := stInstance.NodeCopy.(*ast.StructInstantiation)
		right := stType.NodeCopy.(*ast.StructType)

		if len(left.Fields) > len(right.Fields) {
			*diags = append(*diags, LineDiagnostic{Message: "Cannot convert " + sc.Left.Value + " to " + sc.StType.Value + " since " + sc.Left.Value + " has more values than" + sc.StType.Value, Line: sc.Line()})
			return nil, nil
		}

		rightnames := map[string]bool{}
		for _, field := range right.Fields {
			rightnames[field.Value] = true
			scope.vals[sc.Left.Value+"."+field.Value] = &Initialised{
				Line:   field.Line(),
				Column: getStartColumn(field.Token),
				Kind:   UNKNOWN,
			}
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

func getInfixType(val ast.Node, scope *Scope, diags *[]LineDiagnostic) InitType {
	var res InitType
	if ide, ok := val.(*ast.Identifier); ok && scope.Exists(ide.Value) {
		res = scope.Lookup(ide.Value).Kind
	} else if fa, ok := val.(*ast.FieldAccess); ok {
		str := fieldAccessStr(fa, fa.Left.String())
		if scope.Exists(str) {
			res = scope.Lookup(str).Kind
		} else {
			res = GetInitType(val, scope)
		}
	} else if inf, ok := val.(*ast.InfixExpression); ok {
		res = walkInfix(inf, scope, diags)
	} else {
		res = GetInitType(val, scope)
	}
	return res
}

func walkInfix(n *ast.InfixExpression, scope *Scope, diags *[]LineDiagnostic) InitType {
	left := n.Left
	right := n.Right
	op := n.Operator

	var LeftType = getInfixType(left, scope, diags)
	var RightType = getInfixType(right, scope, diags)

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

func getToken(expr ast.Expression) (token.Token, bool) {
	v := reflect.ValueOf(expr)

	if v.Kind() != reflect.Ptr || v.IsNil() {
		return token.Token{}, false
	}

	v = v.Elem()

	if v.Kind() != reflect.Struct {
		return token.Token{}, false
	}

	field := v.FieldByName("Token")
	if !field.IsValid() || !field.CanInterface() {
		return token.Token{}, false
	}

	tok, ok := field.Interface().(token.Token)
	return tok, ok
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
	case *ast.PrefixExpression:
		if n.Operator != "!" && n.Operator != "-" && n.Operator != "..." {
			*diags = append(*diags, LineDiagnostic{Message: "Cannot use " + n.Operator + " in prefix expression", Line: n.Line(), Token: n.Token})
		}
		runWalk(n.Right, scope, NIL)
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

			if m, ok := Modulemap[path]; ok {
				for _, exp := range m.Exports {
					scope.vals[exp.Name] = exp.Init
				}
			} else {
				program, modulename, extradiags, isStd, p := GetImport(n.Value, n.Line())
				if isStd {
					path = p
				}

				logging.Logger.Println("Path:", path, "Module name:", modulename)

				if extradiags.Line != -1 {
					*diags = append(*diags, extradiags)
				}

				// Show LSP error if import has errors
				if program != nil {
					exports := ProcessImport(program, modulename, path)

					logging.Logger.Println("Processed import", modulename, path)
					logging.Logger.Println("Going through exports of import")
					for _, exp := range exports {
						scope.vals[exp.Name] = exp.Init
					}
					logging.Logger.Println("Gone through exports")
					Modulemap[path] = &Module{Name: path, Exports: exports}
				}
			}
			(*dependencies)[path] = true
		}

	case *ast.AssignExpression:
		runWalk(n.Left, scope, ASSIGNER)
		runWalk(n.Value, scope, ASSIGNING)
	case *ast.IndexExpression:
		leftType := GetInitType(n.Left, scope)
		if leftType == ARRAY {
			runWalk(n.Left, scope, ARRAY_NAME)
			if GetInitType(n.Index, scope) != INT {
				*diags = append(*diags, LineDiagnostic{Message: "Cannot use " + n.Index.String() + " as index in index expression. Should be INT, got " + InitTypeToString[GetInitType(n.Index, scope)], Line: n.Index.Line()})
			}
		} else if leftType == HASH {
			runWalk(n.Left, scope, HASH_NAME)
			indType := GetInitType(n.Index, scope)
			if indType != STRING && indType != INT && indType != BOOL && indType != ARRAY && indType != HASH && indType != FLOAT {
				*diags = append(*diags, LineDiagnostic{Message: "Cannot use " + n.Index.String() + " as index in hash index expression. got " + InitTypeToString[GetInitType(n.Index, scope)], Line: n.Index.Line()})
			}
		} else if leftType == STRING {
			runWalk(n.Left, scope, NIL)
			if GetInitType(n.Index, scope) != INT {
				*diags = append(*diags, LineDiagnostic{Message: "Cannot use " + n.Index.String() + " as index in string index expression. Should be INT, got " + InitTypeToString[GetInitType(n.Index, scope)], Line: n.Index.Line()})
			}
		} else if leftType != UNKNOWN {
			runWalk(n.Left, scope, NIL)
			*diags = append(*diags, LineDiagnostic{Message: "Cannot use " + n.Left.String() + " of type " + InitTypeToString[GetInitType(n.Left, scope)] + " as ARRAY/HASH/STRING in index expression", Line: n.Left.Line(), Token: n.Token})
		} else {
			runWalk(n.Left, scope, NIL)
		}
	case *ast.IndexAssignExpression:

		if GetInitType(n.Left, scope) == ARRAY {
			runWalk(n.Left, scope, ARRAY_NAME)
			if GetInitType(n.Index, scope) != INT {
				*diags = append(*diags, LineDiagnostic{Message: "Cannot use " + n.Index.String() + " as index in index assign expression. Should be INT, got " + InitTypeToString[GetInitType(n.Index, scope)], Line: n.Index.Line()})
			}
		} else if GetInitType(n.Left, scope) == HASH {
			runWalk(n.Left, scope, HASH_NAME)
			indType := GetInitType(n.Index, scope)
			if indType != STRING && indType != INT && indType != BOOL && indType != ARRAY && indType != HASH && indType != FLOAT {
				*diags = append(*diags, LineDiagnostic{Message: "Cannot use " + n.Index.String() + " as index in hash index assign expression. got " + InitTypeToString[GetInitType(n.Index, scope)], Line: n.Index.Line()})
			}
		} else if GetInitType(n.Left, scope) == STRING {
			runWalk(n.Left, scope, NIL)
			if GetInitType(n.Index, scope) != INT {
				*diags = append(*diags, LineDiagnostic{Message: "Cannot use " + n.Index.String() + " as index in string index assign expression. Should be INT, got " + InitTypeToString[GetInitType(n.Index, scope)], Line: n.Index.Line()})
			}
		} else if GetInitType(n.Left, scope) != UNKNOWN {
			runWalk(n.Left, scope, NIL)
			*diags = append(*diags, LineDiagnostic{Message: "Cannot use " + n.Left.String() + " of type " + InitTypeToString[GetInitType(n.Left, scope)] + " as ARRAY/HASH/STRING in index expression", Line: n.Left.Line(), Token: n.Token})
		} else {
			runWalk(n.Left, scope, NIL)
		}
		runWalk(n.Value, scope, ASSIGNING)
	case *ast.SliceExpression:
		leftType := GetInitType(n.Left, scope)
		if leftType == ARRAY {
			runWalk(n.Left, scope, ARRAY_NAME)
		} else if leftType == STRING {
			runWalk(n.Left, scope, NIL)
		} else if leftType != UNKNOWN {
			runWalk(n.Left, scope, NIL)
			*diags = append(*diags, LineDiagnostic{Message: "Cannot use " + n.Left.String() + " of type " + InitTypeToString[leftType] + " as ARRAY/STRING in slice expression", Line: n.Left.Line(), Token: n.Token})
		} else {
			runWalk(n.Left, scope, NIL)
		}
		runWalk(n.IndexStart, scope, ARRAY_INDEX)
		runWalk(n.IndexEnd, scope, ARRAY_INDEX)
	case *ast.HashLiteral:
		for k, v := range n.Pairs {
			keyType := GetInitType(k, scope)
			if keyType != INT && keyType != STRING && keyType != FLOAT && keyType != UNKNOWN {
				*diags = append(*diags, LineDiagnostic{Message: "Unusable as hash key: " + InitTypeToString[keyType], Line: k.Line(), Token: n.Token})
			}
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
		t := GetInitType(n.Ranging, scope)
		if id, ok := n.Ranging.(*ast.Identifier); ok && scope.Exists(id.Value) {
			t = scope.Lookup(id.Value).Kind
		}
		if t != INT && t != FLOAT && t != ARRAY && t != HASH && t != STRING && t != UNKNOWN {
			*diags = append(*diags, LineDiagnostic{Message: "Cannot range over " + InitTypeToString[t], Line: n.Ranging.Line()})
		}
	case *ast.Defer:
		runWalk(n.Exp, scope, NIL)
	case *ast.LetStatement:
		// We get the identifier on the left hand side
		identstr := ""
		var letident *ast.Identifier
		if ident, ok := n.Left.(*ast.Identifier); ok {
			letident = ident
			scope.vals[ident.Value] = &Initialised{
				Line:     n.Left.Line(),
				Column:   getStartColumn(ident.Token),
				NodeCopy: n.Value,
				Kind:     GetInitType(n.Value, scope),
			}
			identstr = ident.Value
		} else if fa, ok := n.Left.(*ast.FieldAccess); ok {
			leftval := scope.Lookup(fa.Left.String())
			if r, ok := fa.Right.(*ast.Identifier); ok && scope.Exists(fa.Left.String()) && leftval.Kind == STRUCT {
				if fn, ok := n.Value.(*ast.FunctionLiteral); ok {
					// create a new enclosed scope and walk the function
					newscope := NewEnclosedScope(scope)
					*scopes = append(*scopes, newscope)
					for _, param := range fn.Parameters {
						newscope.vals[param.Value] = &Initialised{
							Line:   n.Line(),
							Column: getStartColumn(param.Token),
							Kind:   UNKNOWN,
						}
					}
					newscope.vals[fa.Left.String()] = &Initialised{Line: leftval.Line, Column: leftval.Column, Kind: UNKNOWN}
					str := leftval.NodeCopy.(*ast.StructType)

					leftval.Extras[fa.Right.String()] = Position{Line: fa.Line(), StartColumn: getStartColumn(r.Token), EndColumn: r.Token.Column}

					for _, field := range str.Fields {
						newscope.vals[fa.Left.String()+"."+field.Value] = &Initialised{Line: leftval.Line, Column: leftval.Column, Kind: UNKNOWN}
					}
					runWalk(fn.Body, newscope, NIL)
					break
				}
			}
		} else if arr, ok := n.Left.(*ast.ArrayLiteral); ok {
			if _, ok := n.Value.(*ast.CallExpression); !ok {
				if pe, ok := n.Value.(*ast.PrefixExpression); !ok || pe.Operator != "..." {
					*diags = append(*diags, LineDiagnostic{Message: "Cannot use " + n.Value.String() + " as value for multivalue let statement", Line: n.Value.Line(), Token: arr.Token})
				}
			}

			for _, el := range arr.Elements {
				if id, ok := el.(*ast.Identifier); ok {
					scope.vals[id.Value] = &Initialised{
						Line:   id.Line(),
						Column: getStartColumn(id.Token),
						Kind:   UNKNOWN,
					}
				} else if fa, ok := el.(*ast.FieldAccess); ok {
					scope.vals[fieldAccessStr(fa, fa.Left.String())] = &Initialised{
						Line:   fa.Line(),
						Column: getStartColumn(fa.Token),
						Kind:   UNKNOWN,
					}
				} else {
					*diags = append(*diags, LineDiagnostic{Message: "Cannot use " + el.String() + " on left side of let statement", Line: el.Line(), Token: arr.Token})
				}
			}
		}

		// If it is a struct instantiation we want to be adding different initialisations for <variable-name>.<struct-field>
		if si, ok := n.Value.(*ast.StructInstantiation); ok {
			if identstr != "" {
				rWalk := func(n ast.Node, r RoleType, c ...Context) {
					if len(c) > 0 {
						runWalk(n, scope, r, c[0])
					} else {
						runWalk(n, scope, r)
					}
				}
				walkStructInstantiations(context, si, identstr, scope, diags, rWalk, Position{Line: n.Left.Line(), EndColumn: letident.Token.Column, StartColumn: getStartColumn(letident.Token)}, nil)
			}
		} else if sc, ok := n.Value.(*ast.StructTypeConversion); ok {
			st, fields := evalStructConversion(sc, diags, scope)
			if st != nil && len(fields) > 0 {
				sifields := map[string]ast.Expression{}
				si := &ast.StructInstantiation{LineNum: n.Left.Line()}
				for ex, pos := range st.Extras {
					scope.vals[identstr+"."+ex] = &Initialised{
						Line:   pos.Line,
						Column: pos.StartColumn,
						Kind:   FUNCTION,
					}
				}
				stType := st.NodeCopy.(*ast.StructType)
				si.Left = &ast.Identifier{Value: stType.Name.Value}

				for name, t := range fields {
					tok, fine := getToken(t)
					var col int
					if fine {
						col = getStartColumn(tok)
					} else {
						col = 1
					}
					scope.vals[identstr+"."+name] = &Initialised{
						Line:     sc.Line(),
						Column:   col,
						NodeCopy: t,
						Kind:     GetInitType(t, scope),
					}
					sifields[name] = t
				}
				si.Fields = sifields
				scope.vals[identstr] = &Initialised{Line: n.Left.Line(), Column: getStartColumn(letident.Token), Kind: STRUCT_INSTANCE, NodeCopy: si}
			}
		} else {
			// Walk the other side of the let statement
			runWalk(n.Value, scope, ASSIGNING)
			idstr := ""
			if id, ok := n.Value.(*ast.Identifier); ok && scope.Exists(id.Value) {
				idstr = id.Value
			} else if fa, ok := n.Value.(*ast.FieldAccess); ok && scope.Exists(fa.String()) {
				idstr = fa.String()
			}

			if idstr != "" {
				t := scope.Lookup(idstr)
				// TODO: Might be able to remove

				if t.Kind == STRUCT_INSTANCE {
					si := t.NodeCopy.(*ast.StructInstantiation)
					rWalk := func(n ast.Node, r RoleType, c ...Context) {
						// Don't want to be getting errors from import file stuff
						if t.ImportFile == "" {
							if len(c) > 0 {
								runWalk(n, scope, r, c[0])
							} else {
								runWalk(n, scope, r)
							}
						}
					}
					// TODO: see if it is an imported struct
					walkStructInstantiations(context, si, identstr, scope, diags, rWalk, Position{Line: n.Left.Line(), EndColumn: letident.Token.Column, StartColumn: getStartColumn(letident.Token)}, t)

				} else {
					scope.vals[identstr] = &Initialised{
						Line:   n.Left.Line(),
						Column: getStartColumn(letident.Token),
						Kind:   t.Kind,
					}
				}
			}

		}
	case *ast.StructInstantiation:
		if context.letIdent != "" {
			rWalk := func(n ast.Node, r RoleType, c ...Context) {
				if len(c) > 0 {
					runWalk(n, scope, r, c[0])
				} else {
					runWalk(n, scope, r)
				}
			}
			walkStructInstantiations(context, n, context.letIdent, scope, diags, rWalk, Position{Line: n.Left.Line(), EndColumn: n.Token.Column, StartColumn: getStartColumn(n.Token)}, nil)
		} else {
			if id, ok := n.Left.(*ast.Identifier); ok {
				runWalk(id, scope, STRUCT_TYPE)
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

					for name, val := range n.Fields {
						if _, ok := fieldmap[name]; !ok {
							*diags = append(*diags, LineDiagnostic{Message: "Struct type " + id.Value + " has no field " + name, Line: n.Line()})
							break
						}
						runWalk(val, scope, NIL)
					}
				}
			}
		}

	case *ast.StructType:
		// record struct type initialisation
		scope.vals[n.Name.Value] = &Initialised{
			Line:     n.Line(),
			Column:   getStartColumn(n.Name.Token),
			Kind:     GetInitType(n, scope),
			NodeCopy: n,
			Extras:   map[string]Position{},
		}

	case *ast.FunctionLiteral:
		// create a new enclosed scope and walk the function
		newscope := NewEnclosedScope(scope)
		*scopes = append(*scopes, newscope)
		for _, param := range n.Parameters {
			newscope.vals[param.Value] = &Initialised{
				Line:   n.Line(),
				Column: getStartColumn(param.Token),
				Kind:   UNKNOWN,
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
		if rn, ok := n.Condition.(*ast.RangeExpression); ok {
			if rn.Val1 != nil {
				kind := UNKNOWN
				switch GetInitType(rn.Ranging, scope) {
				case STRING:
					kind = INT
				case INT:
					kind = INT
				case ARRAY:
					kind = INT
				}

				newscope.vals[rn.Val1.Value] = &Initialised{
					Line:   rn.Val1.Line(),
					Column: getStartColumn(rn.Val1.Token),
					Kind:   kind,
				}
			}
			if rn.Val2 != nil {
				kind := UNKNOWN
				if GetInitType(rn.Ranging, scope) == STRING {
					kind = STRING
				}
				newscope.vals[rn.Val2.Value] = &Initialised{
					Line:   rn.Val2.Line(),
					Column: getStartColumn(rn.Val2.Token),
					Kind:   kind,
				}
			}
		}
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
		if n.Value == "_" {
			break
		}
		if context.isStd && strings.HasPrefix(n.Value, "GOSTDCALL") {
			break
		}

		// record identifier use
		i := Ident{Val: n.Value, Line: n.Line(), Token: n.Token, UseType: context.role}
		if context.role == INFIXED {
			i.NodeCopy = context.lastNode
		}
		scope.idents = append(scope.idents, i)
	case *ast.FieldAccess:
		if i, ok := n.Left.(*ast.Identifier); ok {
			if scope.Exists(i.Value) {
				if scope.Lookup(i.Value).Kind == UNKNOWN {
					break
				}
			}
			// record field access use as identifier
			i := fieldAccess(n, context, scope, diags, i.Value, func(n ast.Node) { runWalk(n, scope, NIL) }) //Ident{Val: n.String(), Line: n.Line(), UseType: context.role}
			if context.role == INFIXED {
				i.NodeCopy = context.lastNode
			}
			scope.idents = append(scope.idents, i)
		} else {
			runWalk(n.Left, scope, NIL)
			if sc, ok := n.Left.(*ast.StructTypeConversion); ok {
				if id, ok := n.Right.(*ast.Identifier); ok {
					st, fields := evalStructConversion(sc, diags, scope)
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

func fieldAccessStr(fa *ast.FieldAccess, curr string) string {
	if id, ok := fa.Right.(*ast.Identifier); ok {
		curr += "." + id.Value
		return curr
	} else if f, ok := fa.Right.(*ast.FieldAccess); ok {
		if id, ok := f.Left.(*ast.Identifier); ok {
			curr += "." + id.Value
			return fieldAccessStr(f, curr)
		} else {
			return ""
		}
	} else if ie, ok := fa.Right.(*ast.IndexExpression); ok {
		if id, ok := ie.Left.(*ast.Identifier); ok {
			curr += "." + id.Value
			return curr
		} else {
			return ""
		}
	} else if se, ok := fa.Right.(*ast.SliceExpression); ok {
		if id, ok := se.Left.(*ast.Identifier); ok {
			curr += "." + id.Value
			return curr
		} else {
			return ""
		}
	} else if iae, ok := fa.Right.(*ast.IndexAssignExpression); ok {
		if id, ok := iae.Left.(*ast.Identifier); ok {
			curr += "." + id.Value
			return curr
		} else {
			return ""
		}
	} else {
		return ""
	}
}

// Give: hello.bob.jeff
// It returns and ident of hello.bob.jeff
// Give: hello.bob[2]
// it returns ident of hello.bob with use of ARRAY
func fieldAccess(fa *ast.FieldAccess, context Context, scope *Scope, diags *[]LineDiagnostic, curr string, runWalk func(n ast.Node)) Ident {
	if id, ok := fa.Right.(*ast.Identifier); ok {
		curr += "." + id.Value
		return Ident{
			Val:     curr,
			Line:    fa.Line(),
			Token:   token.Token{Type: fa.Token.Type, Line: fa.Line(), Column: id.Token.Column, Literal: curr},
			UseType: context.role,
		}
	} else if f, ok := fa.Right.(*ast.FieldAccess); ok {
		if id, ok := f.Left.(*ast.Identifier); ok {
			curr += "." + id.Value
			return fieldAccess(f, context, scope, diags, curr, runWalk)
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
				if val.Kind == ARRAY && GetInitType(ie.Index, scope) != INT {
					*diags = append(*diags, LineDiagnostic{Message: "Cannot use " + ie.Index.String() + " as index in index expression. Should be INT, got " + InitTypeToString[GetInitType(ie.Index, scope)], Line: ie.Index.Line()})
				} else if val.Kind == HASH {
					indType := GetInitType(ie.Index, scope)
					if indType != STRING && indType != INT && indType != BOOL && indType != ARRAY && indType != HASH && indType != FLOAT {
						*diags = append(*diags, LineDiagnostic{Message: "Cannot use " + ie.Index.String() + " as index in hash index expression. got " + InitTypeToString[GetInitType(ie.Index, scope)], Line: ie.Index.Line()})
					}
				} else if val.Kind == STRING && GetInitType(ie.Index, scope) != INT {
					*diags = append(*diags, LineDiagnostic{Message: "Cannot use " + ie.Index.String() + " as index in string index expression. Should be INT, got " + InitTypeToString[GetInitType(ie.Index, scope)], Line: ie.Index.Line()})
				}
				if val.Kind == HASH {
					ut = HASH_NAME
				}
			}
			return Ident{
				Val:     curr,
				Line:    fa.Line(),
				Token:   token.Token{Type: fa.Token.Type, Line: fa.Line(), Column: id.Token.Column, Literal: curr},
				UseType: ut,
			}
		} else {
			*diags = append(*diags, LineDiagnostic{Message: "Cannot use " + ie.Left.String() + " In field access", Line: ie.Left.Line()})
			return Ident{}
		}
	} else if iae, ok := fa.Right.(*ast.IndexAssignExpression); ok {
		if id, ok := iae.Left.(*ast.Identifier); ok {
			curr += "." + id.Value
			exists := scope.Exists(curr)
			ut := ARRAY_NAME
			if exists {
				val := scope.Lookup(curr)
				if val.Kind == ARRAY && GetInitType(iae.Index, scope) != INT {
					*diags = append(*diags, LineDiagnostic{Message: "Cannot use " + iae.Index.String() + " as index in index expression. Should be INT, got " + InitTypeToString[GetInitType(iae.Index, scope)], Line: iae.Index.Line()})
				} else if val.Kind == HASH {
					indType := GetInitType(iae.Index, scope)
					if indType != STRING && indType != INT && indType != BOOL && indType != ARRAY && indType != HASH && indType != FLOAT {
						*diags = append(*diags, LineDiagnostic{Message: "Cannot use " + iae.Index.String() + " as index in hash index expression. got " + InitTypeToString[GetInitType(iae.Index, scope)], Line: iae.Index.Line()})
					}
				} else if val.Kind == STRING && GetInitType(iae.Index, scope) != INT {
					*diags = append(*diags, LineDiagnostic{Message: "Cannot use " + iae.Index.String() + " as index in string index expression. Should be INT, got " + InitTypeToString[GetInitType(iae.Index, scope)], Line: iae.Index.Line()})
				}
				if val.Kind == HASH {
					ut = HASH_NAME
				}
			}
			runWalk(iae.Value)
			return Ident{
				Val:     curr,
				Line:    fa.Line(),
				Token:   token.Token{Type: fa.Token.Type, Line: fa.Line(), Column: id.Token.Column, Literal: curr},
				UseType: ut,
			}
		} else {
			*diags = append(*diags, LineDiagnostic{Message: "Cannot use " + iae.Left.String() + " In field access", Line: iae.Left.Line()})
			return Ident{}
		}
	} else if se, ok := fa.Right.(*ast.SliceExpression); ok {
		if id, ok := se.Left.(*ast.Identifier); ok {
			curr += "." + id.Value
			exists := scope.Exists(curr)
			ut := ARRAY_NAME
			if exists {
				val := scope.Lookup(curr)
				if val.Kind == ARRAY || val.Kind == STRING {
					if GetInitType(se.IndexStart, scope) != INT {
						*diags = append(*diags, LineDiagnostic{Message: "Cannot use " + se.IndexStart.String() + " as index in slice index expression. Should be INT, got " + InitTypeToString[GetInitType(se.IndexStart, scope)], Line: se.IndexStart.Line()})
					}

					if GetInitType(se.IndexEnd, scope) != INT {
						*diags = append(*diags, LineDiagnostic{Message: "Cannot use " + se.IndexEnd.String() + " as index in index expression. Should be INT, got " + InitTypeToString[GetInitType(se.IndexEnd, scope)], Line: se.IndexEnd.Line()})
					}
				}
			}
			return Ident{
				Val:     curr,
				Line:    fa.Line(),
				Token:   token.Token{Type: fa.Token.Type, Line: fa.Line(), Column: id.Token.Column, Literal: curr},
				UseType: ut,
			}
		} else {
			*diags = append(*diags, LineDiagnostic{Message: "Cannot use " + se.Left.String() + " In fseld access", Line: se.Left.Line()})
			return Ident{}
		}
	} else {
		*diags = append(*diags, LineDiagnostic{Message: "Cannot use " + ie.Left.String() + " In field access", Line: ie.Left.Line()})
		return Ident{}
	}
}

func diag(Message string, line int, lines []string, sev int, token ...token.Token) lsp.Diagnostic {
	if line < 1 {
		return lsp.Diagnostic{}
	}
	seve := lsp.SeverityError
	if sev == 1 {
		seve = lsp.SeverityWarning
	}

	startC := 0
	endC := len(lines[line-1])
	if len(token) > 0 {
		tok := token[0]
		startC = int(math.Max(float64((tok.Column - len(tok.Literal))), 0))
		endC = int(math.Min(float64(tok.Column), float64(endC)))
	}

	return lsp.Diagnostic{
		Range: lsp.Range{
			Start: lsp.Position{
				Line:      line - 1,
				Character: startC,
			},
			End: lsp.Position{
				Line:      line - 1,
				Character: endC,
			},
		},

		Severity: &seve,
		Source:   "bolt",
		Message:  Message,
	}
}

func CheckInitsAndIdents(program *ast.Program, lines []string, path string, uri lsp.DocumentURI, isStd bool) []lsp.Diagnostic {
	dependencies := map[string]bool{}
	linediags := []LineDiagnostic{}
	mainScope := NewScope()
	scopes := []*Scope{}
	for _, stmt := range program.Statements {
		if !IsNil(stmt) {
			walk(stmt, mainScope, &scopes, &dependencies, &linediags, Context{inFor: false, letIdent: "", lastNode: nil, isStd: isStd})
		}
	}
	ModuleDependencies[path] = dependencies

	diags := []lsp.Diagnostic{}
	for _, d := range linediags {
		diags = append(diags, diag(d.Message, d.Line, lines, 0))
	}
	scopes = append(scopes, mainScope)
	logging.Logger.Println("Scopes", len(scopes))

	identLines := []LocationInit{}

	for _, scope := range scopes {
		logging.Logger.Println("new scope")
		for letident, init := range scope.vals {
			if init.Kind == STRUCT_INSTANCE {
				if init.NodeCopy == nil {
					continue
				}
				logging.Logger.Println("Struct instance init", letident, init, init.NodeCopy, InitTypeToString[GetInitType(init.NodeCopy, scope)])
				si, ok := init.NodeCopy.(*ast.StructInstantiation)
				if !ok {
					logging.Logger.Println("Could not convert", letident, "with init", init, "to struct instance")
					continue
				}

				if scope.Exists(si.Left.String()) {
					stType := scope.Lookup(si.Left.String())
					if len(stType.Extras) != 0 {
						for method, pos := range stType.Extras {
							scope.vals[letident].Extras[method] = Position{Line: pos.Line, StartColumn: pos.StartColumn, EndColumn: pos.EndColumn}
							scope.vals[letident+"."+method] = &Initialised{
								Line:   pos.Line,
								Column: pos.StartColumn,
								Kind:   FUNCTION,
							}
						}
					}
				}
			}
		}
		for _, ident := range scope.idents {
			if _, ok := builtins[ident.Val]; !ok && !scope.Exists(ident.Val) {
				diags = append(diags, diag("Identifier not found: "+ident.Val, ident.Line, lines, 0, ident.Token))
			} else {
				if _, ok := builtins[ident.Val]; ok {
					if ident.UseType != FUNCTION_NAME {
						diags = append(diags, diag("Cannot use builtin: "+ident.Val+" as non-function", ident.Line, lines, 0, ident.Token))
					}
				} else if init, ok := RoleToInitType[ident.UseType]; ok {
					it := scope.Lookup(ident.Val)
					if it.Kind != init && init != UNKNOWN && it.Kind != UNKNOWN {
						diags = append(diags, diag("Cannot use "+ident.Val+" which is "+InitTypeToString[it.Kind]+" as "+InitTypeToString[init], ident.Line, lines, 0, ident.Token))
					}
				}
				identLines = append(identLines, LocationInit{Pos: Position{Line: ident.Line, StartColumn: ident.Token.Column - (len(ident.Token.Literal) - 1), EndColumn: ident.Token.Column}, Init: scope.Lookup(ident.Val), Name: ident.Val, Scope: scope})
			}
		}
	}

	logging.Logger.Println("Checked inits and idents")
	DocToIdentLines[uri] = DocumentIdentLines{
		Idents: identLines,
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
	MODULE:          "MODULE",
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
