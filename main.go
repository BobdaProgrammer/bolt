package main

import (
	"bolt/evaluator"
	"bolt/lexer"
	"bolt/object"
	"bolt/parser"
	"bolt/repl"
	"fmt"
	"os"
	"os/user"
)

func main() {
	user, err := user.Current()
	if err != nil {
		panic(err)
	}
	fmt.Printf("Hello %s! This is the bolt programming language\n", user.Username)
	if len(os.Args) < 2 {
		repl.Start()
	} else {
		file := os.Args[1]
		dat, err := os.ReadFile(file)
		if err != nil {
			panic("couldn't read file")
		}
		l := lexer.New(string(dat))
		p := parser.New(l)
		program := p.ParseProgram()

		if len(p.Errors()) != 0 {
			printParserErrors(p.Errors())
			return
		}

		env := object.NewEnvironment(false)
		evaluated := evaluator.Eval(program, env)
		if evaluated != nil {
			fmt.Println(fmt.Sprint(evaluated.Inspect(), "\n"))
		}
	}
}

func printParserErrors(errors []string) {
	for _, msg := range errors {
		fmt.Println("\t" + msg + "\n")
	}
}
