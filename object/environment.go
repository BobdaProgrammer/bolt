package object

func NewEnclosedEnvironment(outer *Environment, inFor bool) *Environment {
	env := NewEnvironment(inFor)
	env.outer = outer
	return env
}

func NewEnvironment(inFor bool) *Environment {
	s := make(map[string]Object)
	return &Environment{store: s, outer: nil, InFor: inFor, Exports: make(map[string]Object), Exporting: false}
}

type Environment struct {
	store     map[string]Object
	outer     *Environment
	InFor     bool
	Exports   map[string]Object
	Exporting bool
}

func (e *Environment) Get(name string) (Object, bool) {
	obj, ok := e.store[name]
	if !ok && e.outer != nil {
		obj, ok = e.outer.Get(name)
	}
	return obj, ok
}

func (e *Environment) Set(name string, val Object) Object {
	e.store[name] = val
	return val
}
