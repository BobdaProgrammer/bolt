package evaluator

import (
	"bolt/object"
	"bolt/utils"
	"bufio"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
)

var (
	lineNum int
)

func SetBuiltinLineNum(l int) {
	lineNum = l
}

func log(args ...object.Object) object.Object {

	for _, arg := range args {
		fmt.Print(arg.Inspect())
		fmt.Print(" ")
	}
	fmt.Print("\n")

	return NULL
}

// wna = wrong number of args
func wna(e, g int, fun string) object.Object {
	return newError("Wrong number of arguments. expected=%d, got=%d, func=%s - line=%d", e, g, fun, lineNum)
}

var builtins = map[string]*object.Builtin{
	"len": {
		Fn: func(args ...object.Object) object.Object {
			if len(args) != 1 {
				return wna(1, len(args), "len(")
			}
			switch arg := args[0].(type) {
			case *object.String:
				return &object.Integer{Value: int64(len(arg.Value))}
			case *object.Array:
				return &object.Integer{Value: int64(len(arg.Elements))}
			case *object.Hash:
				return &object.Integer{Value: int64(len(arg.Pairs))}
			default:
				return newError("Argument to `len` not supported, got %s", args[0].Type())
			}
		},
	},
	"push": {
		// Currently, push updates the array as well as returning it
		Fn: func(args ...object.Object) object.Object {
			if len(args) != 2 {
				return wna(2, len(args), "push(")
			}
			if args[0].Type() != object.ARRAY_OBJ {
				return newError("'push' requires array object as first arguemnt. got=%s", args[0].Type())
			}

			arr := args[0].(*object.Array)
			newArr := make([]object.Object, len(arr.Elements))
			copy(newArr, arr.Elements)
			if args[1].Type() == object.SPREAD_OBJ {
				spread := args[1].(*object.Spread)
				for _, el := range spread.Elements {
					newArr = append(newArr, el)
				}
			} else {
				newArr = append(newArr, args[1])
			}
			return &object.Array{Elements: newArr}
		},
	},
	"delete": {
		Fn: func(args ...object.Object) object.Object {
			if len(args) != 2 {
				return wna(2, len(args), "delete(")
			}
			val := args[0]
			switch val := val.(type) {
			case *object.Hash:
				key, ok := args[1].(object.Hashable)
				if !ok {
					return newError("cannot use %s as hashkey delete value in builtin 'delete' - line=%d", args[1].Inspect(), lineNum)
				}

				delete(val.Pairs, key.HashKey())
			case *object.Array:
				newArr := val.Elements
				for i, el := range val.Elements {
					if bool, ok := utils.EvalInfixExpression("==", el, args[1], lineNum).(*object.Boolean); ok {
						if bool.Value {
							newArr = append(val.Elements[0:i], val.Elements[i+1:len(val.Elements)]...)
							break
						}
					}
				}
				val.Elements = newArr
			default:
				return newError("builtin 'delete' expects argument to be type HASH or ARRAY, got=%s - line=%d", val.Type(), lineNum)
			}
			return NULL
		},
	},
	"panic": {
		Fn: func(args ...object.Object) object.Object {
			log(&object.String{Value: fmt.Sprintf("PANIC - LINE=%d", lineNum)})
			log(args...)
			os.Exit(0)
			return NULL
		},
	},
	"log": {
		Fn: func(args ...object.Object) object.Object {
			return log(args...)
		},
	},
	"input": {
		Fn: func(args ...object.Object) object.Object {
			if len(args) > 1 {
				return newError("builtin 'input' expects 0 or 1 arguements, got %d - line=%d", len(args), lineNum)
			}
			if len(args) == 1 {
				val := args[0]
				if str, ok := val.(*object.String); ok {
					fmt.Print(str.Value)
				} else {
					return newError("builtin 'input' expects argument to be STRING, got %s - line=%d", val.Type(), lineNum)
				}
			}
			reader := bufio.NewReader(os.Stdin)
			text, err := reader.ReadString('\n')
			if err != nil {
				return newError("failed to read input: %s", err)
			}

			text = strings.TrimSuffix(text, "\n")
			text = strings.TrimSuffix(text, "\r")

			return &object.String{Value: text}
		},
	},
	"keys": {
		Fn: func(args ...object.Object) object.Object {
			if len(args) != 1 {
				return wna(1, len(args), "keys(")
			}
			dict := args[0].(*object.Hash)
			arr := []object.Object{}
			for _, val := range dict.Pairs {
				arr = append(arr, val.Key)
			}
			return &object.Array{Elements: arr}
		},
	},
	"typeof": {
		Fn: func(args ...object.Object) object.Object {
			if len(args) != 1 {
				return wna(1, len(args), "typeof(")
			}
			res := "UNKNOWN"
			switch args[0].Type() {
			case object.INTEGER_OBJ:
				res = "INT"
			case object.FLOAT_OBJ:
				res = "FLOAT"
			case object.BOOLEAN_OBJ:
				res = "BOOLEAN"
			case object.NULL_OBJ:
				res = "NULL"
			case object.RETURN_VALUE_OBJ:
				res = "RETURN"
			case object.ERROR_OBJ:
				res = "ERROR"
			case object.FUNCTION_OBJ:
				res = "FUNCTION"
			case object.STRING_OBJ:
				res = "STRING"
			case object.ARRAY_OBJ:
				res = "ARRAY"
			case object.HASH_OBJ:
				res = "HASH"
			case object.BREAK_OBJ:
				res = "BREAK"
			case object.STRUCT_OBJ:
				res = "STRUCT"
			case object.STRUCT_INSTANCE_OBJ:
				val := args[0].(*object.StructInstance)
				res = val.StType.Name.Value
			case object.SPREAD_OBJ:
				res = "SPREAD"
			}
			return &object.String{Value: res}
		},
	},
	"string": {
		Fn: func(args ...object.Object) object.Object {
			if len(args) != 1 {
				return wna(1, len(args), "string(")
			}
			val := args[0]
			switch val := val.(type) {
			case *object.Integer:
				return &object.String{Value: strconv.Itoa(int(val.Value))}
			case *object.String:
				return val
			case *object.Boolean:
				return &object.String{Value: strconv.FormatBool(val.Value)}
			case *object.Float:
				return &object.String{Value: strconv.FormatFloat(val.Value, 'f', -1, 64)}
			default:
				return newError("Cannot convert type %s to type STRING", val)
			}
		},
	},
	"int": {
		Fn: func(args ...object.Object) object.Object {
			if len(args) != 1 {
				return wna(1, len(args), "int(")
			}
			val := args[0]
			switch val := val.(type) {
			case *object.Integer:
				return val
			case *object.String:
				v, err := strconv.Atoi(val.Value)
				if err != nil {
					return NULL
				}
				return &object.Integer{Value: int64(v)}
			case *object.Boolean:
				if val.Value {
					return &object.Integer{Value: 1}
				} else {
					return &object.Integer{Value: 0}
				}
			case *object.Float:
				return &object.Integer{Value: int64(math.Floor(val.Value))}
			default:
				return newError("Cannot convert type %s to type STRING", val)
			}
		},
	},
	"float": {
		Fn: func(args ...object.Object) object.Object {
			if len(args) != 1 {
				return wna(1, len(args), "float(")
			}
			val := args[0]
			switch val := val.(type) {
			case *object.Integer:
				return &object.Float{Value: float64(val.Value)}
			case *object.String:
				v, err := strconv.ParseFloat(val.Value, 64)
				if err != nil {
					return NULL
				}
				return &object.Float{Value: v}
			case *object.Boolean:
				if val.Value {
					return &object.Float{Value: 1.0}
				} else {
					return &object.Float{Value: 0.0}
				}
			case *object.Float:
				return val
			default:
				return newError("Cannot convert type %s to type STRING", val)
			}
		},
	},
}
