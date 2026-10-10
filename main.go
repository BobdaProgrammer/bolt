package main

import (
	"fmt"
	"github.com/BobdaProgrammer/bolt/evaluator"
	"github.com/BobdaProgrammer/bolt/formatter"
	"github.com/BobdaProgrammer/bolt/lexer"
	"github.com/BobdaProgrammer/bolt/object"
	"github.com/BobdaProgrammer/bolt/parser"
	"github.com/BobdaProgrammer/bolt/repl"
	"os"
	"os/user"
)

func main() {
	user, err := user.Current()
	if err != nil {
		panic(err)
	}
	if len(os.Args) < 2 {
		fmt.Printf("Hello %s! This is the bolt programming language\n", user.Username)
		repl.Start()
	} else {
		command := os.Args[1]
		if command == "fmt" {
			file := os.Args[2]
			dat, err := os.ReadFile(file)
			if err != nil {
				panic("couldn't read file")
			}
			l := lexer.New(string(dat))
			p := parser.New(l)
			program := p.ParseProgram()

			fmt.Println(formatter.Format(program))
		} else if command == "run" {
			file := os.Args[2]
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

			env := object.NewEnvironment(false, false, object.PROGRAMENV)
			evaluated := evaluator.Eval(program, env)
			if evaluated != nil && evaluated.Type() == object.ERROR_OBJ {
				fmt.Println(fmt.Sprint(evaluated.Inspect(), "\n"))
			}
		}
	}
}

func printParserErrors(errors []string) {
	for _, msg := range errors {
		fmt.Println("\t" + msg + "\n")
	}
}
