package evaluator

import (
	"bolt/object"
	"fmt"
)

// wna = wrong number of args
func wna(e, g int) object.Object {
	return newError("Wrong number of arguments. expected=%d, got=%d", e, g)
}

var builtins = map[string]*object.Builtin{
	"len": {
		Fn: func(args ...object.Object) object.Object {
			if len(args) != 1 {
				return wna(1, len(args))
			}
			switch arg := args[0].(type) {
			case *object.String:
				return &object.Integer{Value: int64(len(arg.Value))}
			case *object.Array:
				return &object.Integer{Value: int64(len(arg.Elements))}
			default:
				return newError("Argument to `len` not supported, got %s", args[0].Type())
			}
		},
	},
	"push": {
		// Currently, push updates the array as well as returning it
		Fn: func(args ...object.Object) object.Object {
			if len(args) != 2 {
				return wna(2, len(args))
			}
			if args[0].Type() != object.ARRAY_OBJ {
				return newError("'push' requires array object as first arguemnt. got=%s", args[0].Type())
			}

			arr := args[0].(*object.Array)
			newArr := make([]object.Object, len(arr.Elements))
			copy(newArr, arr.Elements)
			newArr = append(newArr, args[1])
			return &object.Array{Elements: newArr}
		},
	},
	"log": {
		Fn: func(args ...object.Object) object.Object {
			for _, arg := range args {
				fmt.Print(arg.Inspect())
				fmt.Print(" ")
			}
			fmt.Print("\n")

			return NULL
		},
	},
	"keys": {
		Fn: func(args ...object.Object) object.Object {
			if len(args) != 1 {
				return wna(1, len(args))
			}
			dict := args[0].(*object.Hash)
			arr := []object.Object{}
			for _, val := range dict.Pairs {
				arr = append(arr, val.Key)
			}
			return &object.Array{Elements: arr}
		},
	},
}
