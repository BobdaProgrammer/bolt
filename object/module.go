package object

type Module struct {
	Env *Environment
}

func CreateNewModule() *Module {
	return &Module{
		Env: NewEnvironment(false),
	}
}
