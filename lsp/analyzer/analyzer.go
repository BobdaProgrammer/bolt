package analyzer

import (
	"math"
	"os"
	"strconv"
	"strings"

	"reflect"

	"path/filepath"

	"github.com/BobdaProgrammer/bolt/ast"
	"github.com/BobdaProgrammer/bolt/evaluator"
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
	Token                        token.Token // optional
}

type LocationInit struct {
	Scope  *Scope
	Name   string
	Init   *Initialised
	Exists bool
	Pos    Position
}

type DocumentIdentLines struct {
	Idents []LocationInit
}

var DocToIdentLines map[lsp.DocumentURI]DocumentIdentLines = map[lsp.DocumentURI]DocumentIdentLines{}
var DocToComments = map[lsp.DocumentURI]map[int]lexer.Comment{}
var DocToScopes = map[lsp.DocumentURI][]*Scope{}

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
	case *ast.PrefixExpression:
		if n.Operator == "..." {
			return SPREAD
		} else if n.Operator == "!" {
			return BOOL
		} else {
			return GetInitType(n.Right, scope)
		}
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
	SPREAD
	MODULE
	UNKNOWN
)

var InitTypeToString map[InitType]string = map[InitType]string{
	INT:             "INT",
	FLOAT:           "FLOAT",
	NULL:            "NULL",
	STRUCT_CONVERT:  "STRUCT CONVERSION",
	BOOL:            "BOOL",
	STRING:          "STRING",
	FUNCTION:        "FUNCTION",
	STRUCT:          "STRUCT",
	STRUCT_INSTANCE: "STRUCT INSTANCE",
	ARRAY:           "ARRAY",
	SPREAD:          "SPREAD",
	HASH:            "HASH",
	MODULE:          "MODULE",
	UNKNOWN:         "UNKNOWN",
}

type Ident struct {
	Val      string
	Line     int
	Token    token.Token
	UseType  RoleType
	NodeCopy ast.Node
}

type NodePos struct {
	Pos      Position
	NodeCopy ast.Node
}

type Initialised struct {
	Line       int
	Column     int
	Kind       InitType
	NodeCopy   ast.Node
	ImportFile string // only for imported vals
	HiddenInit bool   // used if the init is not directly written out (e.g. struct extra or infered field)
	Extras     map[string]NodePos
}

type Scope struct {
	Line   int
	Vals   map[string]*Initialised
	idents []Ident
	Outer  *Scope
}

func (s *Scope) Exists(val string) bool {
	if _, ok := s.Vals[val]; ok {
		return true
	} else if s.Outer != nil {
		return s.Outer.Exists(val)
	} else {
		return false
	}
}
func (s *Scope) Lookup(val string) *Initialised {
	if i, ok := s.Vals[val]; ok {
		return i
	} else if s.Outer != nil {
		return s.Outer.Lookup(val)
	} else {
		return &Initialised{Line: -1, Column: -1, Kind: UNKNOWN}
	}
}

func NewScope(line ...int) *Scope {
	l := -1
	if len(line) > 0 {
		l = line[0]
	}
	return &Scope{
		Vals:   map[string]*Initialised{},
		idents: []Ident{},
		Outer:  nil,
		Line:   l,
	}
}

func NewEnclosedScope(Outer *Scope, line ...int) *Scope {
	l := -1
	if len(line) > 0 {
		l = line[0]
	}

	return &Scope{
		Vals:   map[string]*Initialised{},
		idents: []Ident{},
		Outer:  Outer,
		Line:   l,
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
	PUB
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
			var lex *lexer.Lexer
			lex, program, _ = utils.ProcessModule(file, false)
			if lex != nil {
				commentMap := map[int]lexer.Comment{}
				for _, comment := range lex.Comments() {
					for l := comment.StartLine; l <= comment.EndLine; l++ {
						commentMap[l] = comment
					}
				}
				DocToComments[lsp.DocumentURI(path)] = commentMap
			}
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

	scope.Vals[curr] = &Initialised{Line: pos.Line, Column: pos.StartColumn, Kind: STRUCT_INSTANCE, NodeCopy: si, Extras: map[string]NodePos{}}
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
				scope.Vals[curr+"."+field.Value] = &Initialised{
					Line:   field.Line(),
					Column: getStartColumn(field.Token),
					Kind:   UNKNOWN,
				}
			}

			for method, nodepos := range stru.Extras {
				pos := nodepos.Pos
				scope.Vals[curr].Extras[method] = NodePos{Pos: Position{Line: pos.Line, StartColumn: pos.StartColumn, EndColumn: pos.EndColumn}, NodeCopy: nodepos.NodeCopy}
				scope.Vals[curr+"."+method] = &Initialised{
					Line:     pos.Line,
					Column:   pos.StartColumn,
					Kind:     FUNCTION,
					NodeCopy: nodepos.NodeCopy,
				}
			}

			fmap = fieldmap
		} else if init != nil && init.Extras != nil && len(init.Extras) > 0 {
			for method, nodepos := range init.Extras {
				pos := nodepos.Pos
				scope.Vals[curr].Extras[method] = NodePos{Pos: Position{Line: pos.Line, StartColumn: pos.StartColumn, EndColumn: pos.EndColumn}, NodeCopy: nodepos.NodeCopy}
				scope.Vals[curr+"."+method] = &Initialised{
					Line:     pos.Line,
					NodeCopy: nodepos.NodeCopy,
					Column:   pos.StartColumn,
					Kind:     FUNCTION,
				}
			}
		}
		for name, val := range si.Fields {
			if foundStruct {
				if _, ok := fmap[name.Value]; !ok {
					*diags = append(*diags, LineDiagnostic{Message: "Struct type " + id.Value + " has no field " + name.Value, Line: si.Line()})
					break
				}
			}

			scope.Vals[curr+"."+name.Value] = &Initialised{
				Line:     name.Line(),
				Column:   getStartColumn(name.Token),
				NodeCopy: val,
				Kind:     GetInitType(val, scope),
			}
			// Walk recursively to search for other identifiers
			if sInst, ok := val.(*ast.StructInstantiation); ok {
				walkStructInstantiations(context, sInst, curr+"."+name.Value, scope, diags, runWalk, Position{Line: name.Line(), StartColumn: getStartColumn(name.Token), EndColumn: name.Token.Column}, nil)
			} else {
				runWalk(val, NIL, Context{letIdent: curr + "." + name.Value, inFor: context.inFor, isStd: context.isStd, lastNode: si})
			}
		}
	} else {
		*diags = append(*diags, LineDiagnostic{Message: "Cannot use " + si.Left.String() + "As struct in struct instantiation", Line: si.Left.Line()})
	}

}

func walkStructInstantiationsImport(val ast.Expression, curr string, scope *Scope, privScope *Scope, imp string, isPub bool, pos Position) {
	if sInst, ok := val.(*ast.StructInstantiation); ok {
		if isPub {
			scope.Vals[curr] = &Initialised{Line: pos.Line, Column: pos.StartColumn, Kind: STRUCT_INSTANCE, NodeCopy: sInst, ImportFile: imp, Extras: map[string]NodePos{}}
		}

		privScope.Vals[curr] = &Initialised{Line: pos.Line, Column: pos.StartColumn, Kind: STRUCT_INSTANCE, NodeCopy: sInst, ImportFile: imp, Extras: map[string]NodePos{}}
		if stTypeIdent, ok := sInst.Left.(*ast.Identifier); ok {
			if !privScope.Exists(stTypeIdent.Value) {
				return
			}

			stType := privScope.Lookup(stTypeIdent.Value)
			for method, nodepos := range stType.Extras {
				position := nodepos.Pos
				if isPub {
					scope.Vals[curr].Extras[method] = NodePos{Pos: Position{Line: position.Line, StartColumn: position.StartColumn, EndColumn: position.EndColumn}, NodeCopy: nodepos.NodeCopy}
					scope.Vals[curr+"."+method] = &Initialised{
						Line:       position.Line,
						Column:     position.StartColumn,
						Kind:       FUNCTION,
						NodeCopy:   nodepos.NodeCopy,
						ImportFile: imp,
					}
				}
				privScope.Vals[curr].Extras[method] = NodePos{Pos: Position{Line: position.Line, StartColumn: position.StartColumn, EndColumn: position.EndColumn}, NodeCopy: nodepos.NodeCopy}
				privScope.Vals[curr+"."+method] = &Initialised{
					Line:       position.Line,
					Column:     position.StartColumn,
					Kind:       FUNCTION,
					NodeCopy:   nodepos.NodeCopy,
					ImportFile: imp,
				}
			}
			for field, value := range sInst.Fields {
				if isPub {
					scope.Vals[curr+"."+field.Value] = &Initialised{
						Line:       field.Line(),
						Column:     getStartColumn(field.Token),
						Kind:       GetInitType(value, scope),
						ImportFile: imp,
						NodeCopy:   value,
					}
				}
				privScope.Vals[curr+"."+field.Value] = &Initialised{
					Line:       field.Line(),
					Column:     getStartColumn(field.Token),
					Kind:       GetInitType(value, scope),
					ImportFile: imp,
					NodeCopy:   value,
				}

				if GetInitType(value, scope) == STRUCT_INSTANCE {
					walkStructInstantiationsImport(value, curr+"."+field.Value, scope, privScope, imp, isPub, Position{Line: field.Line(), StartColumn: getStartColumn(field.Token), EndColumn: field.Token.Column})
				}
			}
		}
	}
}

func ProcessImport(p *ast.Program, modulename string, imp string) []Export {
	privScope := NewScope(1)
	sc := NewScope(1)
	for _, stmt := range p.Statements {
		if !IsNil(stmt) {
			WalkImport(stmt, sc, privScope, modulename, false, imp)
		}
	}

	exports := []Export{Export{Name: modulename, Init: &Initialised{Line: 1, Column: 1, ImportFile: imp, Kind: MODULE}}}
	for letident, init := range sc.Vals {
		logging.Logger.Println("Import: ", letident)
		if init.Kind == STRUCT_INSTANCE {
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
				for method, nodepos := range stType.Extras {
					pos := nodepos.Pos
					init.Extras[method] = NodePos{Pos: Position{Line: pos.Line, StartColumn: pos.StartColumn, EndColumn: pos.EndColumn}, NodeCopy: nodepos.NodeCopy}
					exports = append(exports, Export{Name: letident + "." + method, Init: &Initialised{
						Line:       pos.Line,
						Column:     pos.StartColumn,
						Kind:       FUNCTION,
						NodeCopy:   nodepos.NodeCopy,
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
		if IsNil(n.Left) || IsNil(n.Value) {
			break
		}

		var letstr string = ""
		var letident *ast.Identifier
		if i, ok := n.Left.(*ast.Identifier); ok {
			letstr = i.Value
			letident = i
			in := &Initialised{Line: n.Left.Line(), Column: getStartColumn(i.Token), Kind: GetInitType(n.Value, scope), ImportFile: imp, NodeCopy: n.Value}
			if isPub {
				scope.Vals[name+"."+i.Value] = in
			}
			privScope.Vals[name+"."+i.Value] = in
		} else if fa, ok := n.Left.(*ast.FieldAccess); ok && privScope.Exists(fa.Left.String()) {
			if privScope.Lookup(fa.Left.String()).Kind != STRUCT {
				break
			}

			if id, ok := fa.Right.(*ast.Identifier); ok {
				privScope.Lookup(fa.Left.String()).Extras[id.Value] = NodePos{Pos: Position{Line: id.Line(), StartColumn: getStartColumn(id.Token), EndColumn: id.Token.Column}, NodeCopy: n.Value}
			}
		} else if arr, ok := n.Left.(*ast.ArrayLiteral); ok {
			for _, el := range arr.Elements {
				if id, ok := el.(*ast.Identifier); ok {
					if isPub {
						scope.Vals[name+"."+id.Value] = &Initialised{
							Line:       id.Line(),
							Column:     getStartColumn(id.Token),
							ImportFile: imp,
							Kind:       UNKNOWN,
						}
					}

					privScope.Vals[name+"."+id.Value] = &Initialised{
						Line:       id.Line(),
						Column:     getStartColumn(id.Token),
						ImportFile: imp,
						Kind:       UNKNOWN,
					}
				} else if fa, ok := el.(*ast.FieldAccess); ok {
					if isPub {
						scope.Vals[name+"."+fieldAccessStr(fa, fa.Left.String())] = &Initialised{
							Line:       fa.Line(),
							Column:     getStartColumn(fa.Token),
							ImportFile: imp,
							Kind:       UNKNOWN,
						}
					}

					privScope.Vals[name+"."+fieldAccessStr(fa, fa.Left.String())] = &Initialised{
						Line:       fa.Line(),
						Column:     getStartColumn(fa.Token),
						ImportFile: imp,
						Kind:       UNKNOWN,
					}
				}
			}
		}

		if si, ok := n.Value.(*ast.StructInstantiation); ok && letstr != "" {
			walkStructInstantiationsImport(si, name+"."+letstr, scope, privScope, imp, isPub, Position{Line: n.Left.Line(), StartColumn: getStartColumn(letident.Token), EndColumn: letident.Token.Column})
		} else if sc, ok := n.Value.(*ast.StructTypeConversion); ok && letstr != "" {
			st, fields, _ := evalstructConversionImport(sc, scope, privScope, name, name+"."+letstr, isPub, imp)

			logging.Logger.Println("Struct conversion import", st, fields)
			if st != nil && len(fields) > 0 && letident != nil {
				sifields := map[*ast.Identifier]ast.Expression{}
				si := &ast.StructInstantiation{LineNum: n.Left.Line()}
				extras := map[string]NodePos{}
				for ex, nodepos := range st.Extras {
					pos := nodepos.Pos
					extras[ex] = NodePos{Pos: Position{Line: pos.Line, StartColumn: pos.StartColumn, EndColumn: pos.EndColumn}}
					if isPub {
						scope.Vals[name+"."+letstr+"."+ex] = &Initialised{
							Line:       pos.Line,
							Column:     pos.StartColumn,
							NodeCopy:   nodepos.NodeCopy,
							Kind:       FUNCTION,
							ImportFile: imp,
						}
					}
					privScope.Vals[name+"."+letstr+"."+ex] = &Initialised{
						Line:       pos.Line,
						Column:     pos.StartColumn,
						NodeCopy:   nodepos.NodeCopy,
						Kind:       FUNCTION,
						ImportFile: imp,
					}
				}
				stType := st.NodeCopy.(*ast.StructType)
				si.Left = &ast.Identifier{Value: stType.Name.Value}

				for nam, t := range fields {
					// Hidden init since there is no actual source code location where it is initialised and so we don't want to confuse the hover handler or go to definition handler by having multiple identlines in one place
					if isPub {
						scope.Vals[name+"."+letstr+"."+nam.Value] = &Initialised{
							Line:       sc.Line(),
							Column:     getStartColumn(sc.Token),
							NodeCopy:   t,
							Kind:       GetInitType(t, scope),
							HiddenInit: true,
							ImportFile: imp,
						}
					}
					privScope.Vals[name+"."+letstr+"."+nam.Value] = &Initialised{
						Line:       sc.Line(),
						Column:     getStartColumn(sc.Token),
						NodeCopy:   t,
						Kind:       GetInitType(t, scope),
						HiddenInit: true,
						ImportFile: imp,
					}
					sifields[nam] = t
				}
				si.Fields = sifields
				if isPub {
					scope.Vals[name+"."+letstr] = &Initialised{Line: n.Left.Line(), Column: getStartColumn(letident.Token), Kind: STRUCT_INSTANCE, NodeCopy: si, ImportFile: imp, Extras: extras}
				}
				privScope.Vals[name+"."+letstr] = &Initialised{Line: n.Left.Line(), Column: getStartColumn(letident.Token), Kind: STRUCT_INSTANCE, NodeCopy: si, ImportFile: imp, Extras: extras}
			}
		} else if letstr != "" {

			// Walk the other side of the let statement
			idstr := ""
			line := n.Left.Line()
			scol := 1
			ecol := 1
			if id, ok := n.Value.(*ast.Identifier); ok && privScope.Exists(name+"."+id.Value) {
				idstr = name + "." + id.Value
				line = id.Line()
				scol = getStartColumn(id.Token)
				ecol = id.Token.Column
			} else if fa, ok := n.Value.(*ast.FieldAccess); ok {
				if privScope.Exists(name + "." + fa.String()) {
					line = fa.Line()
					scol = getStartColumn(fa.Token)
					ecol = fa.Token.Column
					idstr = name + "." + fa.String()
				}
				rt := checkSpecialFieldAccessImport(fa, privScope, name)
				logging.Logger.Println("field access let statement", fa.String(), InitTypeToString[rt])
				if idstr == "" && rt != UNKNOWN {
					if isPub {
						scope.Vals[name+"."+letstr] = &Initialised{Line: n.Left.Line(), Column: getStartColumn(letident.Token), Kind: rt, NodeCopy: fa}
					}
					privScope.Vals[name+"."+letstr] = &Initialised{Line: n.Left.Line(), Column: getStartColumn(letident.Token), Kind: rt, NodeCopy: fa}
				}
			}

			if idstr != "" {
				t := privScope.Lookup(idstr)

				if t.Kind == STRUCT_INSTANCE {
					si := t.NodeCopy.(*ast.StructInstantiation)
					walkStructInstantiationsImport(si, name+"."+letstr, scope, privScope, imp, isPub, Position{Line: line, StartColumn: scol, EndColumn: ecol})
				} else {
					if isPub {
						scope.Vals[name+"."+letstr] = &Initialised{
							Line:       n.Left.Line(),
							Column:     getStartColumn(letident.Token),
							Kind:       t.Kind,
							ImportFile: imp,
						}
					}
					privScope.Vals[name+"."+letstr] = &Initialised{
						Line:       n.Left.Line(),
						Column:     getStartColumn(letident.Token),
						Kind:       t.Kind,
						ImportFile: imp,
					}
				}
			}

		}
	case *ast.StructType:
		if isPub {
			i := &Initialised{Line: n.Name.Line(), Column: getStartColumn(n.Name.Token), Kind: STRUCT, ImportFile: imp, Extras: map[string]NodePos{}, NodeCopy: n}
			scope.Vals[name+"."+n.Name.Value] = i
		}
		i := &Initialised{Line: n.Name.Line(), Column: getStartColumn(n.Name.Token), Kind: STRUCT, Extras: map[string]NodePos{}, NodeCopy: n, ImportFile: imp}

		privScope.Vals[n.Name.Value] = i

	case *ast.PubStatement:
		WalkImport(n.Stmt, scope, privScope, name, true, imp)
	}
}

func evalstructConversionImport(sc *ast.StructTypeConversion, scope *Scope, privScope *Scope, name string, letIdent string, isPub bool, imp string) (*Initialised, map[*ast.Identifier]ast.Expression, map[string]ast.Expression) {
	leftStr := ""
	var left *ast.StructInstantiation = nil
	if id, ok := sc.Left.(*ast.Identifier); ok {
		leftStr = name + "." + id.Value
		left, ok = getStructInstance(privScope, leftStr, &[]LineDiagnostic{}, sc.Line())
		if !ok {
			return nil, nil, nil
		}
	} else if fa, ok := sc.Left.(*ast.FieldAccess); ok {
		leftStr = name + "." + fa.String()
		left, ok = getStructInstance(privScope, leftStr, &[]LineDiagnostic{}, sc.Line())
		if !ok {
			return nil, nil, nil
		}
	} else if si, ok := sc.Left.(*ast.StructInstantiation); ok {
		left = si
	} else {
		return nil, nil, nil
	}

	rightStr := ""
	var right *ast.StructType = nil
	var stType *Initialised
	if id, ok := sc.StType.(*ast.Identifier); ok {
		rightStr = id.Value
		right, ok = getStructType(privScope, rightStr, &[]LineDiagnostic{}, sc.Line())
		if !ok {
			return nil, nil, nil
		}
		stType = privScope.Lookup(rightStr)
	} else if fa, ok := sc.StType.(*ast.FieldAccess); ok {
		rightStr = fa.String()
		right, ok = getStructType(privScope, rightStr, &[]LineDiagnostic{}, sc.Line())
		if !ok {
			return nil, nil, nil
		}
		stType = privScope.Lookup(rightStr)
	} else {
		return nil, nil, nil
	}

	if len(left.Fields) > len(right.Fields) {
		return nil, nil, nil
	}

	rightnames := map[string]bool{}
	for _, field := range right.Fields {
		rightnames[field.Value] = true
		if letIdent != "" {
			if isPub {
				scope.Vals[letIdent+"."+field.Value] = &Initialised{
					Line:       field.Line(),
					Column:     getStartColumn(field.Token),
					Kind:       UNKNOWN,
					ImportFile: imp,
				}
			}
			privScope.Vals[letIdent+"."+field.Value] = &Initialised{
				Line:       field.Line(),
				Column:     getStartColumn(field.Token),
				Kind:       UNKNOWN,
				ImportFile: imp,
			}
		}
	}

	newMap := map[*ast.Identifier]ast.Expression{}
	strMap := map[string]ast.Expression{}
	for name, val := range left.Fields {
		if _, ok := rightnames[name.Value]; !ok {
			return nil, nil, nil
		} else {
			newMap[name] = val
			strMap[name.Value] = val
		}
	}
	return stType, newMap, strMap
}
func getStructType(scope *Scope, str string, diags *[]LineDiagnostic, line int) (*ast.StructType, bool) {
	if scope.Exists(str) && scope.Lookup(str).Kind == STRUCT && scope.Lookup(str).NodeCopy != nil {
		nc := scope.Lookup(str).NodeCopy
		val, ok := nc.(*ast.StructType)
		if !ok {
			*diags = append(*diags, LineDiagnostic{Message: "Cannot use " + str + " as struct type in struct conversion. got: " + InitTypeToString[GetInitType(nc, scope)], Line: line})
			return nil, false
		} else {
			return val, true
		}
	} else if scope.Exists(str) {
		*diags = append(*diags, LineDiagnostic{Message: "Cannot use " + str + " as struct type in struct conversion. got: " + InitTypeToString[scope.Lookup(str).Kind], Line: line})
		return nil, false
	} else {
		return nil, false
	}
}
func getStructInstance(scope *Scope, str string, diags *[]LineDiagnostic, line int) (*ast.StructInstantiation, bool) {
	if scope.Exists(str) && scope.Lookup(str).Kind == STRUCT_INSTANCE && scope.Lookup(str).NodeCopy != nil {
		nc := scope.Lookup(str).NodeCopy
		val, ok := nc.(*ast.StructInstantiation)
		if !ok {
			*diags = append(*diags, LineDiagnostic{Message: "Cannot use " + str + " as struct instance in struct conversion. got: " + InitTypeToString[GetInitType(nc, scope)], Line: line})
			return nil, false
		} else {
			return val, true
		}
	} else if scope.Exists(str) {
		*diags = append(*diags, LineDiagnostic{Message: "Cannot use " + str + " as struct instance in struct conversion. got: " + InitTypeToString[scope.Lookup(str).Kind], Line: line})
		return nil, false
	} else {
		return nil, false
	}
}

func evalstructConversion(sc *ast.StructTypeConversion, diags *[]LineDiagnostic, scope *Scope, letIdent string) (*Initialised, map[*ast.Identifier]ast.Expression, map[string]ast.Expression) {
	leftStr := ""
	var left *ast.StructInstantiation = nil
	if id, ok := sc.Left.(*ast.Identifier); ok {
		leftStr = id.Value
		left, ok = getStructInstance(scope, leftStr, diags, sc.Line())
		if !ok {
			return nil, nil, nil
		}
	} else if fa, ok := sc.Left.(*ast.FieldAccess); ok {
		leftStr = fa.String()
		left, ok = getStructInstance(scope, leftStr, diags, sc.Line())
		if !ok {
			return nil, nil, nil
		}
	} else if si, ok := sc.Left.(*ast.StructInstantiation); ok {
		left = si
	} else {
		return nil, nil, nil
	}

	rightStr := ""
	var right *ast.StructType = nil
	var stType *Initialised
	if id, ok := sc.StType.(*ast.Identifier); ok {
		rightStr = id.Value
		right, ok = getStructType(scope, rightStr, diags, sc.Line())
		if !ok {
			return nil, nil, nil
		}
		stType = scope.Lookup(rightStr)
	} else if fa, ok := sc.StType.(*ast.FieldAccess); ok {
		rightStr = fa.String()
		right, ok = getStructType(scope, rightStr, diags, sc.Line())
		if !ok {
			return nil, nil, nil
		}
		stType = scope.Lookup(rightStr)
	} else {
		return nil, nil, nil
	}

	if len(left.Fields) > len(right.Fields) {
		*diags = append(*diags, LineDiagnostic{Message: "Cannot convert " + leftStr + " to " + sc.StType.String() + " since " + leftStr + " has more values than" + sc.StType.String(), Line: sc.Line()})
		return nil, nil, nil
	}

	rightnames := map[string]bool{}
	for _, field := range right.Fields {
		rightnames[field.Value] = true
		if letIdent != "" {
			scope.Vals[letIdent+"."+field.Value] = &Initialised{
				Line:   field.Line(),
				Column: getStartColumn(field.Token),
				Kind:   UNKNOWN,
			}
		}
	}

	newMap := map[*ast.Identifier]ast.Expression{}
	strMap := map[string]ast.Expression{}
	for name, val := range left.Fields {
		if _, ok := rightnames[name.Value]; !ok {
			*diags = append(*diags, LineDiagnostic{Message: "Cannot convert " + leftStr + " to " + sc.StType.String() + " since " + leftStr + " has value " + name.Value + " That is not a value of " + sc.StType.String(), Line: sc.Line()})
			return nil, nil, nil
		} else {
			newMap[name] = val
			strMap[name.Value] = val
		}
	}
	return stType, newMap, strMap
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
			} else if RightType != STRING {
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

func walk(n ast.Node, scope *Scope, scopes *[]*Scope, dependencies *map[string]bool, diags *[]LineDiagnostic, context Context, identLines *[]LocationInit, letInits *map[*Scope]map[string]Position) {
	if IsNil(n) {
		return
	}

	runWalk := func(node ast.Node, scope *Scope, rt RoleType, ctx ...Context) {
		if len(ctx) > 0 {
			walk(node, scope, scopes, dependencies, diags, ctx[0], identLines, letInits)
		} else {
			newCtx := context
			newCtx.letIdent = ""
			newCtx.lastNode = n
			newCtx.role = rt
			walk(node, scope, scopes, dependencies, diags, newCtx, identLines, letInits)
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
		if scope.Outer == nil {
			can, err := CanonicalPath(n.Value)
			path := n.Value
			if err == nil {
				path = can
			}
			logging.Logger.Println("Import path: ", path)

			if m, ok := Modulemap[path]; ok {
				for _, exp := range m.Exports {
					scope.Vals[exp.Name] = exp.Init
				}
				*identLines = append(*identLines, LocationInit{Pos: Position{Line: n.Line(), StartColumn: n.Token.Column + 2, EndColumn: n.Token.Column + 2 + len(n.Value) + 1}, Scope: scope, Name: path, Init: &Initialised{Line: 1, Column: 1, ImportFile: path, Kind: MODULE}, Exists: true})
			} else {
				program, modulename, extradiag, isStd, p := GetImport(n.Value, n.Line())
				if isStd {
					path = p
				}

				logging.Logger.Println("Path:", path, "Module name:", modulename)

				if extradiag.Line != -1 {
					*diags = append(*diags, extradiag)
				}

				// Show LSP error if import has errors
				if program != nil {
					exports := ProcessImport(program, modulename, path)

					for _, exp := range exports {
						scope.Vals[exp.Name] = exp.Init
					}

					logging.Logger.Println("DONE IMPORTING", path)
					Modulemap[path] = &Module{Name: path, Exports: exports}
					*identLines = append(*identLines, LocationInit{Pos: Position{Line: n.Line(), StartColumn: n.Token.Column + 2, EndColumn: n.Token.Column + 2 + len(n.Value) + 1}, Scope: scope, Name: modulename, Init: &Initialised{Line: 1, Column: 1, ImportFile: path, Kind: MODULE}, Exists: true})
				}
			}
			(*dependencies)[path] = true
		}

	case *ast.AssignExpression:
		runWalk(n.Left, scope, ASSIGNER)
		runWalk(n.Value, scope, ASSIGNING)
	case *ast.IndexExpression:
		leftType := GetInitType(n.Left, scope)
		runWalk(n.Index, scope, NIL)
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
		runWalk(n.Index, scope, NIL)
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
		runWalk(n.IndexStart, scope, NIL)
		runWalk(n.IndexEnd, scope, NIL)
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
			evalstructConversion(n, diags, scope, "")
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
		if IsNil(n.Value) || IsNil(n.Left) {
			break
		}
		// We get the identifier on the left hand side
		identstr := ""
		var letident *ast.Identifier
		if ident, ok := n.Left.(*ast.Identifier); ok {
			letInit(letInits, scope, ident.Value, context.role, n.Value, Position{Line: ident.Line(), StartColumn: getStartColumn(ident.Token), EndColumn: ident.Token.Column, Token: ident.Token})
			letident = ident
			scope.Vals[ident.Value] = &Initialised{
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
					newscope := NewEnclosedScope(scope, fn.Line())
					*scopes = append(*scopes, newscope)
					for _, param := range fn.Parameters {
						newscope.Vals[param.Value] = &Initialised{
							Line:   n.Line(),
							Column: getStartColumn(param.Token),
							Kind:   UNKNOWN,
						}
					}
					newscope.Vals[fa.Left.String()] = &Initialised{Line: leftval.Line, Column: leftval.Column, Kind: UNKNOWN}
					str := leftval.NodeCopy.(*ast.StructType)

					leftval.Extras[fa.Right.String()] = NodePos{Pos: Position{Line: fa.Line(), StartColumn: getStartColumn(r.Token), EndColumn: r.Token.Column}, NodeCopy: fn}

					for _, field := range str.Fields {
						newscope.Vals[fa.Left.String()+"."+field.Value] = &Initialised{Line: leftval.Line, Column: leftval.Column, Kind: UNKNOWN}
					}
					runWalk(fn.Body, newscope, NIL)
					break
				}
			}
		} else if arr, ok := n.Left.(*ast.ArrayLiteral); ok {
			if _, ok := n.Value.(*ast.CallExpression); !ok && GetInitType(n.Value, scope) != UNKNOWN {
				if pe, ok := n.Value.(*ast.PrefixExpression); !ok || pe.Operator != "..." {
					*diags = append(*diags, LineDiagnostic{Message: "Cannot use " + n.Value.String() + " as value for multivalue let statement. got: " + InitTypeToString[GetInitType(n.Value, scope)], Line: n.Value.Line(), Token: arr.Token})
				}
			}

			for _, el := range arr.Elements {
				if id, ok := el.(*ast.Identifier); ok {
					logging.Logger.Println("Let statement array literal identifier", id.Value, context.role, n.Value, id.Token.Column)
					letInit(letInits, scope, id.Value, context.role, n.Value, Position{Line: id.Line(), StartColumn: getStartColumn(id.Token), EndColumn: id.Token.Column, Token: id.Token})
					scope.Vals[id.Value] = &Initialised{
						Line:   id.Line(),
						Column: getStartColumn(id.Token),
						Kind:   UNKNOWN,
					}
				} else if fa, ok := el.(*ast.FieldAccess); ok {
					scope.Vals[fieldAccessStr(fa, fa.Left.String())] = &Initialised{
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
		} else if sc, ok := n.Value.(*ast.StructTypeConversion); ok && identstr != "" {
			runWalk(sc.StType, scope, STRUCT_TYPE)
			runWalk(sc.Left, scope, STRUCT_INSTANCE_ROLE)
			st, fields, _ := evalstructConversion(sc, diags, scope, identstr)
			if st != nil && len(fields) > 0 {
				sifields := map[*ast.Identifier]ast.Expression{}
				si := &ast.StructInstantiation{LineNum: n.Left.Line()}
				extras := map[string]NodePos{}
				for ex, nodepos := range st.Extras {
					pos := nodepos.Pos
					extras[ex] = NodePos{Pos: Position{Line: pos.Line, StartColumn: pos.StartColumn, EndColumn: pos.EndColumn}}
					scope.Vals[identstr+"."+ex] = &Initialised{
						Line:     pos.Line,
						Column:   pos.StartColumn,
						NodeCopy: nodepos.NodeCopy,
						Kind:     FUNCTION,
					}
				}
				stType := st.NodeCopy.(*ast.StructType)
				si.Left = &ast.Identifier{Value: stType.Name.Value}

				for name, t := range fields {
					// Hidden init since there is no actual source code location where it is initialised and so we don't want to confuse the hover handler or go to definition handler by having multiple identlines in one place
					scope.Vals[identstr+"."+name.Value] = &Initialised{
						Line:       sc.Line(),
						Column:     getStartColumn(sc.Token),
						NodeCopy:   t,
						Kind:       GetInitType(t, scope),
						HiddenInit: true,
					}
					sifields[name] = t
				}
				si.Fields = sifields
				scope.Vals[identstr] = &Initialised{Line: n.Left.Line(), Column: getStartColumn(letident.Token), Kind: STRUCT_INSTANCE, NodeCopy: si, Extras: extras}
			}
		} else if identstr != "" {
			// Walk the other side of the let statement
			isFa := false
			idstr := ""
			if id, ok := n.Value.(*ast.Identifier); ok && scope.Exists(id.Value) {
				idstr = id.Value
			} else if fa, ok := n.Value.(*ast.FieldAccess); ok {
				if scope.Exists(fa.String()) {
					idstr = fa.String()
				}
				isFa = true
				logging.Logger.Println("field access as value of let statement", fa.String())
				rt := evalFieldAccess(fa, scope, Context{letIdent: identstr, inFor: context.inFor, isStd: context.isStd, lastNode: n}, diags, runWalk)
				if idstr == "" {
					scope.Vals[identstr] = &Initialised{Line: n.Left.Line(), Column: getStartColumn(letident.Token), Kind: rt, NodeCopy: fa}
				}
			}
			if !isFa {
				runWalk(n.Value, scope, ASSIGNING)
			}

			if idstr != "" {
				t := scope.Lookup(idstr)

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
					walkStructInstantiations(context, si, identstr, scope, diags, rWalk, Position{Line: n.Left.Line(), EndColumn: letident.Token.Column, StartColumn: getStartColumn(letident.Token)}, t)

				} else {
					scope.Vals[identstr] = &Initialised{
						Line:   n.Left.Line(),
						Column: getStartColumn(letident.Token),
						Kind:   t.Kind,
					}
				}
			}

		} else {
			runWalk(n.Value, scope, NIL)
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
						if _, ok := fieldmap[name.Value]; !ok {
							*diags = append(*diags, LineDiagnostic{Message: "Struct type " + id.Value + " has no field " + name.Value, Line: n.Line()})
							break
						}
						runWalk(val, scope, NIL)
					}
				}
			}
		}

	case *ast.StructType:
		// record struct type initialisation
		scope.Vals[n.Name.Value] = &Initialised{
			Line:     n.Line(),
			Column:   getStartColumn(n.Name.Token),
			Kind:     GetInitType(n, scope),
			NodeCopy: n,
			Extras:   map[string]NodePos{},
		}

	case *ast.FunctionLiteral:
		// create a new enclosed scope and walk the function
		newscope := NewEnclosedScope(scope, n.Line())
		*scopes = append(*scopes, newscope)
		for _, param := range n.Parameters {
			newscope.Vals[param.Value] = &Initialised{
				Line:   n.Line(),
				Column: getStartColumn(param.Token),
				Kind:   UNKNOWN,
			}
		}
		runWalk(n.Body, newscope, NIL)
	case *ast.IfExpression:
		// create a new enclosed scope and walk the if statement and its alternatives
		runWalk(n.Condition, scope, IF_CONDITION)
		newscope := NewEnclosedScope(scope, n.Line())
		*scopes = append(*scopes, newscope)
		runWalk(n.Consequence, newscope, NIL)
		if n.Alternative != nil {
			runWalk(n.Alternative, scope, NIL)
		}
	case *ast.ForExpression:
		// create a new enclosed scope and walk the for loop
		runWalk(n.Condition, scope, NIL)
		newscope := NewEnclosedScope(scope, n.Line())
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

				newscope.Vals[rn.Val1.Value] = &Initialised{
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
				newscope.Vals[rn.Val2.Value] = &Initialised{
					Line:   rn.Val2.Line(),
					Column: getStartColumn(rn.Val2.Token),
					Kind:   kind,
				}
			}
		}
		runWalk(n.Consequence, newscope, NIL, Context{letIdent: "", inFor: true, lastNode: n, isStd: context.isStd})
	case *ast.SwitchExpression:
		// Walk the switch item then each case
		runWalk(n.SwitchItem, scope, NIL)
		for _, cas := range n.Cases {
			// walk the case condition and also the body
			runWalk(cas.Condition, scope, NIL)
			newscope := NewEnclosedScope(scope, cas.Line())
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
		runWalk(n.Stmt, scope, PUB)
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
		if scope.Exists(n.Function.String()) {
			fn := scope.Lookup(n.Function.String())
			if fn.Kind == FUNCTION && fn.NodeCopy != nil {
				function, ok := fn.NodeCopy.(*ast.FunctionLiteral)
				if ok && len(function.Parameters) != len(n.Arguments) {
					*diags = append(*diags, LineDiagnostic{Line: n.Line(), Message: "Function: " + n.Function.String() + " expects " + strconv.Itoa(len(function.Parameters)) + " arguments. Got: " + strconv.Itoa(len(n.Arguments))})
				}
			}
		}
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
		evalFieldAccess(n, scope, context, diags, runWalk)
	}

}

func letInit(letInits *map[*Scope]map[string]Position, scope *Scope, s string, roleType RoleType, expression ast.Expression, pos Position) {
	if roleType == PUB {
		return
	}

	t := GetInitType(expression, scope)
	if t != INT && t != BOOL && t != STRING && t != FLOAT && t != ARRAY && t != HASH && t != SPREAD {
		return
	}

	if _, ok := (*letInits)[scope]; !ok {
		(*letInits)[scope] = map[string]Position{}
	}
	(*letInits)[scope][s] = pos
}

func checkSpecialFieldAccessImport(n *ast.FieldAccess, scope *Scope, name string) InitType {
	if _, ok := n.Left.(*ast.Identifier); ok {
		return UNKNOWN
	} else {
		if sc, ok := n.Left.(*ast.StructTypeConversion); ok {
			if id, ok := n.Right.(*ast.Identifier); ok {
				st, _, strfields := evalstructConversionImport(sc, scope, scope, name, "", false, "")
				logging.Logger.Println("struct conversion field access", sc.String(), st, strfields, "Identifier")
				if len(strfields) != 0 && st != nil {
					_, isfield := strfields[id.Value]
					_, ismethod := st.Extras[id.Value]
					if ismethod {
						return FUNCTION
					} else if isfield {
						return GetInitType(strfields[id.Value], scope)
					}
				}
			}
		}
	}
	return UNKNOWN
}

func evalFieldAccess(n *ast.FieldAccess, scope *Scope, context Context, diags *[]LineDiagnostic, runWalk func(n ast.Node, scope *Scope, rt RoleType, c ...Context)) InitType {
	if IsNil(n.Left) || IsNil(n.Right) {
		return UNKNOWN
	}

	if i, ok := n.Left.(*ast.Identifier); ok {
		if scope.Exists(i.Value) {
			if scope.Lookup(i.Value).Kind == UNKNOWN {
				return UNKNOWN
			}
		}
		// record field access use as identifier
		i, ok := fieldAccess(n, context, scope, diags, i.Value, func(n ast.Node) { runWalk(n, scope, NIL) }) //Ident{Val: n.String(), Line: n.Line(), UseType: context.role}
		if ok {
			if context.role == INFIXED {
				i.NodeCopy = context.lastNode
			}
			scope.idents = append(scope.idents, i)
		}
	} else {
		runWalk(n.Left, scope, NIL)
		if sc, ok := n.Left.(*ast.StructTypeConversion); ok {
			if id, ok := n.Right.(*ast.Identifier); ok {
				st, _, strfields := evalstructConversion(sc, diags, scope, "")
				logging.Logger.Println("struct conversion field access", sc.String(), st, strfields, "Identifier")
				if len(strfields) != 0 && st != nil {
					_, isfield := strfields[id.Value]
					_, ismethod := st.Extras[id.Value]
					if !isfield && !ismethod {
						*diags = append(*diags, LineDiagnostic{Message: id.Value + " is not a value of STRUCT INSTANCE " + sc.Left.String() + " that has been converted to " + sc.StType.String(), Line: n.Right.Line()})
						return UNKNOWN
					} else if ismethod {
						return FUNCTION
					} else if isfield {
						return GetInitType(strfields[id.Value], scope)
					}
				}
			} else {
				*diags = append(*diags, LineDiagnostic{Message: "Cannot use " + n.Right.String() + " on STRUCT INSTANCE", Line: n.Right.Line()})
			}
		}
	}
	return UNKNOWN
}

func fieldAccessStr(fa *ast.FieldAccess, curr string) string {
	if IsNil(fa.Left) || IsNil(fa.Right) {
		return ""
	}
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
func fieldAccess(fa *ast.FieldAccess, context Context, scope *Scope, diags *[]LineDiagnostic, curr string, runWalk func(n ast.Node)) (Ident, bool) {
	if IsNil(fa.Left) || IsNil(fa.Right) {
		return Ident{}, false
	}
	if id, ok := fa.Right.(*ast.Identifier); ok {
		curr += "." + id.Value
		return Ident{
			Val:     curr,
			Line:    fa.Line(),
			Token:   token.Token{Type: fa.Token.Type, Line: fa.Line(), Column: id.Token.Column, Literal: curr},
			UseType: context.role,
		}, true
	} else if f, ok := fa.Right.(*ast.FieldAccess); ok {
		if id, ok := f.Left.(*ast.Identifier); ok {
			curr += "." + id.Value
			return fieldAccess(f, context, scope, diags, curr, runWalk)
		} else {
			*diags = append(*diags, LineDiagnostic{Message: "Cannot use " + f.Left.String() + " In field access", Line: f.Left.Line()})
			return Ident{}, false
		}
	} else if ie, ok := fa.Right.(*ast.IndexExpression); ok {
		runWalk(ie.Index)
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
			}, true
		} else {
			*diags = append(*diags, LineDiagnostic{Message: "Cannot use " + ie.Left.String() + " In field access", Line: ie.Left.Line()})
			return Ident{}, false
		}
	} else if iae, ok := fa.Right.(*ast.IndexAssignExpression); ok {
		runWalk(iae.Index)
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
			}, true
		} else {
			*diags = append(*diags, LineDiagnostic{Message: "Cannot use " + iae.Left.String() + " In field access", Line: iae.Left.Line()})
			return Ident{}, false
		}
	} else if se, ok := fa.Right.(*ast.SliceExpression); ok {
		runWalk(se.IndexStart)
		runWalk(se.IndexEnd)
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
			}, true
		} else {
			*diags = append(*diags, LineDiagnostic{Message: "Cannot use " + se.Left.String() + " In field access", Line: se.Left.Line()})
			return Ident{}, false
		}
	} else {
		*diags = append(*diags, LineDiagnostic{Message: "Cannot use " + fa.Right.String() + " In field access", Line: fa.Right.Line()})
		return Ident{}, false
	}
}

func diag(Message string, line int, lines []string, sev int, tags []lsp.DiagnosticTag, token ...token.Token) lsp.Diagnostic {
	if line < 1 || line > len(lines) {
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

	d := lsp.Diagnostic{
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

	if len(tags) > 0 {
		d.Tags = tags
	}

	return d
}

func CheckInitsAndIdents(program *ast.Program, lines []string, path string, uri lsp.DocumentURI, isStd bool) []lsp.Diagnostic {
	dependencies := map[string]bool{}
	linediags := []LineDiagnostic{}
	mainScope := NewScope(1)
	scopes := []*Scope{}
	identLines := []LocationInit{}
	letInits := map[*Scope]map[string]Position{}
	logging.Logger.Println("program", IsNil(program))
	_, ok := ast.SafeString(program)
	if IsNil(program.Statements) || !ok {
		return []lsp.Diagnostic{}
	}
	for _, stmt := range program.Statements {
		if !IsNil(stmt) {
			walk(stmt, mainScope, &scopes, &dependencies, &linediags, Context{inFor: false, letIdent: "", lastNode: nil, isStd: isStd}, &identLines, &letInits)
		}
	}
	ModuleDependencies[path] = dependencies

	diags := []lsp.Diagnostic{}
	for _, d := range linediags {
		diags = append(diags, diag(d.Message, d.Line, lines, 0, []lsp.DiagnosticTag{}))
	}
	scopes = append(scopes, mainScope)
	logging.Logger.Println("Scopes", len(scopes))

	extractedBuiltins := false
	builtinDest := ""
	DocToScopes[uri] = scopes
	for _, scope := range scopes {
		initNameToInit := map[string]*Initialised{}
		for letident, init := range scope.Vals {
			initNameToInit[letident] = init
			if init.ImportFile == "" && !init.HiddenInit {
				identLines = append(identLines, LocationInit{Pos: Position{Line: init.Line, StartColumn: init.Column, EndColumn: init.Column + (len(letident) - 1)}, Init: init, Name: letident, Scope: scope, Exists: true})
			}
			if init.Kind == STRUCT_INSTANCE {
				if init.NodeCopy == nil {
					continue
				}
				si, ok := init.NodeCopy.(*ast.StructInstantiation)
				if !ok {
					logging.Logger.Println("Could not convert", letident, "with init", init, "to struct instance")
					continue
				}

				if scope.Exists(si.Left.String()) {
					stType := scope.Lookup(si.Left.String())
					if len(stType.Extras) != 0 {
						for method, nodepos := range stType.Extras {
							pos := nodepos.Pos
							scope.Vals[letident].Extras[method] = NodePos{Pos: Position{Line: pos.Line, StartColumn: pos.StartColumn, EndColumn: pos.EndColumn}, NodeCopy: nodepos.NodeCopy}
							scope.Vals[letident+"."+method] = &Initialised{
								Line:     pos.Line,
								Column:   pos.StartColumn,
								NodeCopy: nodepos.NodeCopy,
								Kind:     FUNCTION,
							}
						}
					}
				}
			}
		}
		for _, ident := range scope.idents {
			if l, ok := letInits[scope][ident.Val]; ok && (scope.Exists(ident.Val) && scope.Lookup(ident.Val).Line == l.Line) {
				letInits[scope][ident.Val] = Position{Line: -2, StartColumn: -2, EndColumn: -2}
			} else if scope.Outer != nil {
				sc := scope.Outer
				for sc != nil {
					if _, ok := letInits[sc]; ok {
						logging.Logger.Println("let inits", ident.Val, letInits[sc][ident.Val], letInits[sc][ident.Val].Line, sc.Lookup(ident.Val).Line)
						if l, ok := letInits[sc][ident.Val]; ok && (sc.Exists(ident.Val) && sc.Lookup(ident.Val).Line == l.Line) {
							letInits[sc][ident.Val] = Position{Line: -2, StartColumn: -2, EndColumn: -2}
							break
						}
					}

					sc = sc.Outer
				}
			}

			if _, ok := builtins[ident.Val]; !ok && !scope.Exists(ident.Val) {
				// Add non exisiting ident to list
				identLines = append(identLines, LocationInit{Pos: Position{Line: ident.Line, StartColumn: getStartColumn(ident.Token), EndColumn: ident.Token.Column}, Init: &Initialised{NodeCopy: nil, Kind: UNKNOWN, Line: ident.Line, Column: getStartColumn(ident.Token)}, Name: ident.Val, Scope: scope, Exists: false})
				diags = append(diags, diag("Identifier not found: "+ident.Val, ident.Line, lines, 0, []lsp.DiagnosticTag{}, ident.Token))
			} else {
				if _, ok := builtins[ident.Val]; ok && !scope.Exists(ident.Val) {
					if !extractedBuiltins {
						dest := evaluator.ExtractBuiltins()
						builtinDest = dest
						if _, ok := Modulemap[dest]; !ok {
							file := utils.ReadFile(dest)
							var lex *lexer.Lexer
							lex, program, _ = utils.ProcessModule(file, false)
							if lex != nil {
								commentMap := map[int]lexer.Comment{}
								for _, comment := range lex.Comments() {
									for l := comment.StartLine; l <= comment.EndLine; l++ {
										commentMap[l] = comment
									}
								}
								DocToComments[lsp.DocumentURI(builtinDest)] = commentMap
							}
							exports := []Export{}
							for _, stmt := range program.Statements {
								if !IsNil(stmt) {
									if p, ok := stmt.(*ast.PubStatement); ok {
										if l, ok := p.Stmt.(*ast.LetStatement); ok {
											col := getStartColumn(l.Token)
											if id, ok := l.Left.(*ast.Identifier); ok {
												col = getStartColumn(id.Token)
											}
											exports = append(exports, Export{Name: l.Left.String(), Init: &Initialised{
												Line:       l.Line(),
												Column:     col,
												NodeCopy:   l.Value,
												Kind:       GetInitType(l.Value, NewScope()),
												ImportFile: dest,
											}})
										}
									}
								}
							}
							Modulemap[dest] = &Module{Name: "", Exports: exports}
						}
					}
					if builtins, ok := Modulemap[builtinDest]; ok {
						for _, builtin := range builtins.Exports {
							if builtin.Name == ident.Val {
								identLines = append(identLines, LocationInit{Pos: Position{Line: ident.Line, StartColumn: ident.Token.Column - (len(ident.Token.Literal) - 1), EndColumn: ident.Token.Column}, Init: &Initialised{Line: builtin.Init.Line, Column: builtin.Init.Column, Kind: FUNCTION, NodeCopy: builtin.Init.NodeCopy, ImportFile: builtinDest}, Name: builtin.Name, Scope: scope, Exists: true})
							}
						}
					} else {
						identLines = append(identLines, LocationInit{Pos: Position{Line: ident.Line, StartColumn: ident.Token.Column - (len(ident.Token.Literal) - 1), EndColumn: ident.Token.Column}, Init: &Initialised{Line: 1, Column: 1, Kind: FUNCTION, ImportFile: builtinDest}, Name: ident.Val, Scope: scope, Exists: true})
					}

					if ident.UseType != FUNCTION_NAME {
						diags = append(diags, diag("Cannot use builtin: "+ident.Val+" as non-function", ident.Line, lines, 0, []lsp.DiagnosticTag{}, ident.Token))
					}
				} else if init, ok := RoleToInitType[ident.UseType]; ok {
					it := scope.Lookup(ident.Val)
					if it.Kind != init && init != UNKNOWN && it.Kind != UNKNOWN {
						diags = append(diags, diag("Cannot use "+ident.Val+" which is "+InitTypeToString[it.Kind]+" as "+InitTypeToString[init], ident.Line, lines, 0, []lsp.DiagnosticTag{}, ident.Token))
					}
				}
				if scope.Exists(ident.Val) {
					identLines = append(identLines, LocationInit{Pos: Position{Line: ident.Line, StartColumn: ident.Token.Column - (len(ident.Token.Literal) - 1), EndColumn: ident.Token.Column}, Init: scope.Lookup(ident.Val), Name: ident.Val, Scope: scope, Exists: true})
				}
			}
		}

	}

	for _, scope := range scopes {
		for initname, pos := range letInits[scope] {
			if pos.Line != -2 && pos.StartColumn != -2 && pos.EndColumn != -2 {
				if pos.Token != (token.Token{}) {
					diags = append(diags, diag("Unused variable "+initname, pos.Line, lines, 1, []lsp.DiagnosticTag{lsp.DiagnosticTag(1)}, pos.Token)) // 1 is tag unecessary
				} else {
					diags = append(diags, diag("Unused variable "+initname, pos.Line, lines, 1, []lsp.DiagnosticTag{lsp.DiagnosticTag(1)})) // 1 is tag unecessary
				}
			}
		}
	}

	logging.Logger.Println("Checked inits and idents")
	DocToIdentLines[uri] = DocumentIdentLines{
		Idents: identLines,
	}

	return diags
}

func Parse(source string) (*ast.Program, []lsp.Diagnostic, map[int]lexer.Comment) {
	l := lexer.New(source)
	p := parser.New(l)
	program := p.ParseProgram()
	lines := strings.Split(source, "\n")

	diags := []lsp.Diagnostic{}
	if len(p.Errors()) != 0 {
		errs := p.ParserErrors()
		sev := lsp.SeverityError
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
	}

	commentMap := map[int]lexer.Comment{}
	for _, comment := range l.Comments() {
		for l := comment.StartLine; l <= comment.EndLine; l++ {
			commentMap[l] = comment
		}
	}

	return program, diags, commentMap
}
