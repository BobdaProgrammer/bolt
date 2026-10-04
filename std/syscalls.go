package std

import (
	"bolt/object"
	"io"
	"os"
	"path/filepath"
)

var openFiles map[string]*os.File = map[string]*os.File{}

func closeAll() {
	for _, file := range openFiles {
		file.Close()
	}
}

func syscallClose(args []object.Object) object.Object {
	if len(args) != 1 {
		return &object.UserError{Message: "syscallclose wants one argument"}
	}
	filenamestr, ok := args[0].(*object.String)
	if !ok {
		return &object.UserError{Message: "syscallclose can't extract filename string"}
	}
	file, ok := openFiles[filenamestr.Value]
	if !ok {
		return &object.UserError{Message: "syscallclose can't find file to open"}
	}
	file.Close()
	return &object.Null{}
}

func syscallAppend(args []object.Object) object.Object {
	if len(args) != 2 {
		return &object.UserError{Message: "append expects 2 arguments"}
	}

	filenamestr, ok := args[0].(*object.String)
	if !ok {
		return &object.UserError{Message: "append expects a filename as its first argument"}
	}

	file, ok := openFiles[filenamestr.Value]
	if !ok {
		return &object.UserError{Message: "file is not open: " + filenamestr.Value}
	}

	content, ok := args[1].(*object.String)
	if !ok {
		return &object.UserError{Message: "append expects a string as its second argument"}
	}

	_, err := file.Seek(0, 2)
	if err != nil {
		return &object.UserError{Message: "failed to seek to end of file: " + err.Error()}
	}

	_, err = file.WriteString(content.Value)
	if err != nil {
		return &object.UserError{Message: "failed to append to file: " + err.Error()}
	}

	return &object.Null{}
}

func syscallWrite(args []object.Object) object.Object {
	if len(args) != 2 {
		return &object.UserError{Message: "write expects 2 arguments"}
	}

	filenamestr, ok := args[0].(*object.String)
	if !ok {
		return &object.UserError{Message: "write expects a filename as its first argument"}
	}

	file, ok := openFiles[filenamestr.Value]
	if !ok {
		return &object.UserError{Message: "file is not open: " + filenamestr.Value}
	}

	content, ok := args[1].(*object.String)
	if !ok {
		return &object.UserError{Message: "write expects a string as its second argument"}
	}

	err := file.Truncate(0)
	if err != nil {
		return &object.UserError{Message: "failed to write to file: " + err.Error()}
	}

	_, err = file.Seek(0, 0)
	if err != nil {
		return &object.UserError{Message: "failed to seek to beginning of file: " + err.Error()}
	}

	_, err = file.WriteString(content.Value)
	if err != nil {
		return &object.UserError{Message: "failed to write to file: " + err.Error()}
	}

	return &object.Null{}
}

func syscallRead(args []object.Object) object.Object {
	if len(args) != 1 {
		return &object.UserError{Message: "read expects 1 argument"}
	}

	filenamestr, ok := args[0].(*object.String)
	if !ok {
		return &object.UserError{Message: "read expects a filename"}
	}

	file, ok := openFiles[filenamestr.Value]
	if !ok {
		return &object.UserError{Message: "file is not open: " + filenamestr.Value}
	}

	_, err := file.Seek(0, 0)
	if err != nil {
		return &object.UserError{Message: "failed to seek to beginning of file: " + err.Error()}
	}

	content, err := io.ReadAll(file)
	if err != nil {
		return &object.UserError{Message: "failed to read file: " + err.Error()}
	}

	return &object.String{Value: string(content)}
}

// (File struct instantiation, file name)
func syscallOpen(args []object.Object) object.Object {
	if len(args) != 2 {
		return &object.UserError{Message: "open expects 2 arguments"}
	}

	filenamestr, ok := args[1].(*object.String)
	if !ok {
		return &object.UserError{Message: "open expects a filename as its second argument"}
	}

	filename := filenamestr.Value

	file, err := os.OpenFile(
		filename,
		os.O_RDWR|os.O_CREATE,
		0644,
	)
	if err != nil {
		return &object.UserError{Message: "failed to open file: " + err.Error()}
	}

	structinstance, ok := args[0].(*object.StructInstance)
	if !ok {
		file.Close()
		return &object.UserError{Message: "open expects a File struct as its first argument"}
	}

	path, err := filepath.Abs(filename)
	if err != nil {
		file.Close()
		return &object.UserError{Message: "failed to resolve file path: " + err.Error()}
	}

	structinstance.Fields["name"] = &object.String{Value: file.Name()}
	structinstance.Fields["path"] = &object.String{Value: path}

	openFiles[path] = file

	return &object.Null{}
}

func Syscall(args ...object.Object) object.Object {
	if len(args) == 0 {
		return &object.Error{Message: "syscall: missing operation"}
	}

	op, ok := args[0].(*object.String)
	if !ok {
		return &object.Error{Message: "syscall: operation must be a string"}
	}

	switch op.Value {
	case "open":
		return syscallOpen(args[1:])

	case "read":
		return syscallRead(args[1:])

	case "write":
		return syscallWrite(args[1:])

	case "append":
		return syscallAppend(args[1:])

	case "close":
		return syscallClose(args[1:])

	case "closeall":
		closeAll()
		return &object.Null{}
	//
	// case "stat":
	// 	return syscallStat(args[1:])
	//
	// case "mkdir":
	// 	return syscallMkdir(args[1:])
	//
	// case "remove":
	// 	return syscallRemove(args[1:])
	//
	// case "rename":
	// 	return syscallRename(args[1:])
	//
	default:
		return &object.Error{
			Message: "unknown syscall: " + op.Value,
		}
	}
}
