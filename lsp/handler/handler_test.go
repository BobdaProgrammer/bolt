package handler_test

import (
	"context"
	"testing"
	"time"

	"github.com/BobdaProgrammer/bolt/lsp/handler"
	"github.com/owenrumney/go-lsp/servertest"
)

func TestInitialize(t *testing.T) {
	h := servertest.New(t, handler.New())

	if h.InitResult.ServerInfo == nil || h.InitResult.ServerInfo.Name != "bolt" {
		t.Error("expected server info to be set")
	}
}

func TestHover(t *testing.T) {
	h := servertest.New(t, handler.New())

	h.DidOpen("file:///test.txt", "bolt", "hello world")

	hover, err := h.Hover("file:///test.txt", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if hover == nil {
		t.Fatal("expected hover result")
	}
}

func TestCompletion(t *testing.T) {
	h := servertest.New(t, handler.New())

	h.DidOpen("file:///test.txt", "bolt", "hello")

	result, err := h.Completion("file:///test.txt", 0, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Items) == 0 {
		t.Error("expected at least one completion item")
	}
}

func TestDiagnostics(t *testing.T) {
	h := servertest.New(t, handler.New())

	h.DidOpen("file:///test.txt", "bolt", "TODO fix this")
	h.DidSave("file:///test.txt")

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	diags, err := h.WaitForDiagnostics(ctx, "file:///test.txt")
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) == 0 {
		t.Error("expected diagnostics for TODO")
	}
}
