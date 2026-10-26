package handler_test

import (
	"testing"

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
