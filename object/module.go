package object

type Module struct {
	Env *Environment
}

func CreateNewModule(isStd bool) *Module {
	return &Module{
		Env: NewEnvironment(false, isStd, PROGRAMENV),
	}
}
