package object

import (
	"bolt/ast"
	"bytes"
	"fmt"
	"hash/fnv"
	"math"
	"strings"
)

type BuiltinFunction func(args ...Object) Object

type ObjectType string

const (
	NULL_OBJ            = "NULL"
	INTEGER_OBJ         = "INTEGER"
	BOOLEAN_OBJ         = "BOOLEAN"
	ERROR_OBJ           = "ERROR"
	RETURN_VALUE_OBJ    = "RETURN_VALUE"
	FUNCTION_OBJ        = "FUNCTION"
	STRING_OBJ          = "STRING"
	ARRAY_OBJ           = "ARRAY"
	BUILTIN_OBJ         = "BUILTIN"
	HASH_OBJ            = "HASH"
	FLOAT_OBJ           = "FLOAT"
	STRUCT_OBJ          = "STRUCT"
	STRUCT_INSTANCE_OBJ = "STRUCT_INSTANCE"
	SPREAD_OBJ          = "SPREAD"
	BREAK_OBJ           = "BREAK"
)

type Builtin struct {
	Fn BuiltinFunction
}

func (b *Builtin) Type() ObjectType { return BUILTIN_OBJ }
func (b *Builtin) Inspect() string  { return "builtin function" }

type Object interface {
	Type() ObjectType
	Inspect() string
}

type Integer struct {
	Value int64
}

func (i *Integer) Inspect() string  { return fmt.Sprintf("%d", i.Value) }
func (i *Integer) Type() ObjectType { return INTEGER_OBJ }

type Float struct {
	Value float64
}

func (f *Float) Inspect() string  { return fmt.Sprintf("%g", f.Value) }
func (f *Float) Type() ObjectType { return FLOAT_OBJ }

type Boolean struct {
	Value bool
}

func (b *Boolean) Inspect() string  { return fmt.Sprintf("%t", b.Value) }
func (b *Boolean) Type() ObjectType { return BOOLEAN_OBJ }

type Null struct{}

func (n *Null) Inspect() string  { return "null" }
func (n *Null) Type() ObjectType { return NULL_OBJ }

type ReturnValue struct {
	Value Object
}

func (rv *ReturnValue) Inspect() string  { return rv.Value.Inspect() }
func (rv *ReturnValue) Type() ObjectType { return RETURN_VALUE_OBJ }

type Error struct {
	Message string
}

func (e *Error) Inspect() string  { return "ERROR: " + e.Message }
func (e *Error) Type() ObjectType { return ERROR_OBJ }

type Function struct {
	Parameters []*ast.Identifier
	Body       *ast.BlockStatement
	Env        *Environment
}

func (f *Function) Type() ObjectType {
	return FUNCTION_OBJ
}
func (f *Function) Inspect() string {
	var out bytes.Buffer
	params := []string{}
	for _, p := range f.Parameters {
		params = append(params, p.String())
	}

	out.WriteString("fn")
	out.WriteString("(")
	out.WriteString(strings.Join(params, ","))
	out.WriteString(") {\n")
	out.WriteString(f.Body.String() + "\n}")

	return out.String()
}

type String struct {
	Value string
}

func (s *String) Type() ObjectType { return STRING_OBJ }
func (s *String) Inspect() string  { return s.Value }

type Array struct {
	Elements []Object
}

func (ao *Array) Type() ObjectType { return ARRAY_OBJ }
func (ao *Array) Inspect() string {
	els := []string{}

	for _, el := range ao.Elements {
		els = append(els, el.Inspect())
	}

	return "[" + strings.Join(els, ", ") + "]"
}

type HashKey struct {
	Type  ObjectType
	Value uint64
}

func (b *Boolean) HashKey() HashKey {
	var value uint64

	if b.Value {
		value = 1
	} else {
		value = 0
	}

	return HashKey{Type: b.Type(), Value: value}
}

func (i *Integer) HashKey() HashKey {
	return HashKey{Type: i.Type(), Value: uint64(i.Value)}
}

func (f *Float) HashKey() HashKey {
	return HashKey{Type: f.Type(), Value: math.Float64bits(f.Value)}
}

func (s *String) HashKey() HashKey {
	h := fnv.New64a()
	h.Write([]byte(s.Value))
	return HashKey{Type: s.Type(), Value: h.Sum64()}
}

type HashPair struct {
	Key   Object
	Value Object
}

type Hash struct {
	Pairs map[HashKey]HashPair
}

func (h *Hash) Type() ObjectType { return HASH_OBJ }
func (h *Hash) Inspect() string {
	pairs := []string{}
	for _, pair := range h.Pairs {
		pairs = append(pairs, fmt.Sprintf("%s: %s", pair.Key.Inspect(), pair.Key.Inspect()))
	}
	return "{" + strings.Join(pairs, ", ") + "}"
}

type Hashable interface {
	HashKey() HashKey
}

type Break struct{}

func (b *Break) Inspect() string  { return "break" }
func (b *Break) Type() ObjectType { return BREAK_OBJ }

type StructType struct {
	Name   *ast.Identifier
	Fields []*ast.Identifier
}

func (st *StructType) Inspect() string {
	fields := []string{}
	for _, f := range st.Fields {
		fields = append(fields, f.Value)
	}
	return "struct " + st.Name.Value + " { " + strings.Join(fields, ", ") + "}"
}
func (st *StructType) Type() ObjectType { return STRUCT_OBJ }

type StructInstance struct {
	Fields map[string]Object
	StType *StructType
}

func (si *StructInstance) Inspect() string {
	fields := []string{}
	for id, val := range si.Fields {
		fields = append(fields, id+" : "+val.Inspect())
	}
	return "{" + strings.Join(fields, ", ") + "}"
}

func (si *StructInstance) Type() ObjectType { return STRUCT_INSTANCE_OBJ }

type Spread struct {
	Elements []Object
}

func (sp *Spread) Type() ObjectType { return SPREAD_OBJ }
func (sp *Spread) Inspect() string {
	els := []string{}

	for _, el := range sp.Elements {
		els = append(els, el.Inspect())
	}

	return strings.Join(els, ", ")
}
