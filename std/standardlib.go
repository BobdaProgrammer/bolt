package std

import (
	"embed"
	"fmt"
	"strings"

	"bolt/ast"
	"bolt/object"
	"bolt/utils"
)

//go:embed *.bolt net/*.bolt
var files embed.FS

var packages map[string]string = map[string]string{
	"math":     "math.bolt",
	"fs":       "fs.bolt",
	"os":       "os.bolt",
	"strings":  "strings.bolt",
	"testing":  "testing.bolt",
	"time":     "time.bolt",
	"net/http": "net/http.bolt",
	"net/net":  "net/net.bolt",
	"io":       "io.bolt",
}

var StandardGoCallFunctions map[string]object.Object = map[string]object.Object{
	"lower":   &object.Builtin{Fn: lower},
	"upper":   &object.Builtin{Fn: upper},
	"syscall": &object.Builtin{Fn: Syscall},
}

func newError(format string, a ...interface{}) *object.Error {
	return &object.Error{Message: fmt.Sprintf(format, a...)}
}

// wna = wrong number of args
func wna(e, g int, fun string) object.Object {
	return newError("Wrong number of arguments in go standard call. expected=%d, got=%d, func=%s", e, g, fun)
}

func lower(args ...object.Object) object.Object {
	if len(args) != 1 {
		return wna(1, len(args), "lower")
	}

	if str, ok := args[0].(*object.String); ok {
		return &object.String{Value: strings.ToLower(str.Value)}
	} else {
		return &object.Null{}
	}
}

func upper(args ...object.Object) object.Object {
	if len(args) != 1 {
		return wna(1, len(args), "upper")
	}

	if str, ok := args[0].(*object.String); ok {
		return &object.String{Value: strings.ToUpper(str.Value)}
	} else {
		return &object.Null{}
	}
}

func StandardGoCallFunction(fun string, args []object.Object) object.Object {
	if fn, ok := StandardGoCallFunctions[fun]; ok {
		builtin := fn.(*object.Builtin)
		return builtin.Fn(args...)
	}

	return &object.Error{
		Message: "Not a golang standard call func: " + fun,
	}
}

func IsStandardLib(name string) bool {
	_, ok := packages[name]
	return ok
}

func LoadStdPkg(name string) (*ast.Program, *object.Module) {
	pkg := packages[name]

	data, err := files.ReadFile(pkg)
	if err != nil {
		panic(err)
	}

	return utils.ProcessModule(string(data), true)
}
