package utils

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BobdaProgrammer/bolt/ast"
	"github.com/BobdaProgrammer/bolt/lexer"
	"github.com/BobdaProgrammer/bolt/object"
	"github.com/BobdaProgrammer/bolt/parser"
)

func FileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func ReadFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

func ProcessModule(file string, isStd bool) (*lexer.Lexer, *ast.Program, *object.Module) {
	l := lexer.New(file)
	p := parser.New(l)
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		printParserErrors(p.Errors())
		return nil, nil, &object.Module{}
	}

	mod := object.CreateNewModule(isStd)
	return l, program, mod
}

func FileName(path string) string {
	filename := filepath.Base(path)
	name := strings.TrimSuffix(filename, filepath.Ext(filename))
	return name
}

func printParserErrors(errors []string) {
	for _, msg := range errors {
		fmt.Println("\t" + msg + "\n")
	}
}
