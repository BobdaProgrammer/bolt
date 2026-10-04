package object

func NewEnclosedEnvironment(outer *Environment, inFor bool, IsStd bool) *Environment {
	env := NewEnvironment(inFor, IsStd)
	env.outer = outer
	return env
}

func NewEnvironment(inFor bool, IsStd bool) *Environment {
	s := make(map[string]Object)
	return &Environment{store: s, outer: nil, InFor: inFor, Exports: make(map[string]Object), Exporting: false, IsStd: IsStd}
}

type Environment struct {
	store     map[string]Object
	outer     *Environment
	InFor     bool
	Exports   map[string]Object
	Exporting bool
	IsStd     bool
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
