package logging

import (
	"log"
	"os"
)

var Logger *log.Logger

func Init() (*os.File, error) {
	home, err := os.UserHomeDir()
	var start string
	if err != nil {
		start = ""
	} else {
		start = home
	}
	file, err := os.OpenFile(
		start+"/.local/share/bolt/bolt-lsp.log",
		os.O_CREATE|os.O_WRONLY|os.O_TRUNC,
		0644,
	)
	if err != nil {
		return nil, err
	}

	Logger = log.New(
		file,
		"[bolt-lsp] ",
		log.Ldate|log.Ltime|log.Lmicroseconds,
	)

	Logger.Println("LSP started")

	return file, nil
}
