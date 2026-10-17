package main

import (
	"context"
	"github.com/BobdaProgrammer/bolt/lsp/handler"
	"github.com/BobdaProgrammer/bolt/lsp/logging"
	"github.com/owenrumney/go-lsp/server"
	"log"
)

func main() {
	file, err := logging.Init()
	if err != nil {
		log.Fatalf("failed to initialise logger: %v", err)
	}
	defer file.Close()
	h := handler.New()
	srv := server.NewServer(h, server.WithDebugUI(":7100"))
	if err := srv.Run(context.Background(), server.RunStdio()); err != nil {
		log.Fatal(err)
	}
}
