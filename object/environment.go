package object

import "bolt/ast"

type EnvType string

const (
	FORENV      = "FOR"
	PROGRAMENV  = "PROGRAM"
	FUNCTIONENV = "FUNCTION"
	IFENV       = "IF"
)

func AddDeferToEnv(def *ast.Defer, env *Environment) {
	if env.Type == PROGRAMENV || env.Type == FUNCTIONENV {
		env.Defers = append(env.Defers, def.Exp)
	} else if env.outer != nil {
		AddDeferToEnv(def, env.outer)
	}
}

func NewEnclosedEnvironment(outer *Environment, InFor bool, IsStd bool, et EnvType) *Environment {
	env := NewEnvironment(InFor, IsStd, et)
	env.outer = outer
	return env
}

func NewEnvironment(InFor bool, IsStd bool, et EnvType) *Environment {
	s := make(map[string]Object)
	e := &Environment{store: s, outer: nil, Type: et, Exports: make(map[string]Object), Exporting: false, IsStd: IsStd, Defers: []ast.Expression{}}
	if InFor {
		e.InFor = true
	} else {
		e.InFor = false
	}
	return e
}

type Environment struct {
	store     map[string]Object
	outer     *Environment
	Type      EnvType
	InFor     bool
	Exports   map[string]Object
	Exporting bool
	IsStd     bool
	Defers    []ast.Expression
}

func (e *Environment) Get(name string) (Object, bool) {
	obj, ok := e.store[name]
	if !ok && e.outer != nil {
		obj, ok = e.outer.Get(name)
	}
	return obj, ok
}

func (e *Environment) AssignSet(name string, val Object) Object {
	if name == "_" {
		return val
	}
	if _, ok := e.store[name]; !ok {
		return e.outer.AssignSet(name, val)
	} else {
		e.store[name] = val
	}
	return val
}

func (e *Environment) Set(name string, val Object) Object {
	if name == "_" {
		return val
	}
	e.store[name] = val
	return val
}
