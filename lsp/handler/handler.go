package handler

import (
	"context"
	"os"
	"strings"

	"github.com/BobdaProgrammer/bolt/ast"
	"github.com/BobdaProgrammer/bolt/lsp/analyzer"
	"github.com/BobdaProgrammer/bolt/lsp/logging"
	"github.com/BobdaProgrammer/bolt/std"
	"github.com/BobdaProgrammer/bolt/utils"
	"github.com/owenrumney/go-lsp/document"
	"github.com/owenrumney/go-lsp/lsp"
	"github.com/owenrumney/go-lsp/server"
)

type Completion struct {
	Label         string
	Kind          lsp.CompletionItemKind
	Detail        string
	Documentation string
}

var completions = []Completion{
	// Keywords
	{
		Label:         "let",
		Kind:          lsp.CompletionItemKindKeyword,
		Detail:        "variable declaration",
		Documentation: "Declares a new variable.",
	},
	{
		Label:         "pub",
		Kind:          lsp.CompletionItemKindKeyword,
		Detail:        "public declaration",
		Documentation: "Makes a declaration publicly accessible from an imported module.",
	},
	{
		Label:         "fn",
		Kind:          lsp.CompletionItemKindKeyword,
		Detail:        "function",
		Documentation: "Declares a function.",
	},
	{
		Label:         "struct",
		Kind:          lsp.CompletionItemKindKeyword,
		Detail:        "struct declaration",
		Documentation: "Declares a struct type.",
	},
	{
		Label:         "new",
		Kind:          lsp.CompletionItemKindKeyword,
		Detail:        "struct instantiation",
		Documentation: "Creates a new struct value.",
	},
	{
		Label:         "if",
		Kind:          lsp.CompletionItemKindKeyword,
		Detail:        "conditional",
		Documentation: "Executes a block when a condition is true.",
	},
	{
		Label:         "else",
		Kind:          lsp.CompletionItemKindKeyword,
		Detail:        "conditional branch",
		Documentation: "Provides an alternative branch for an if expression.",
	},
	{
		Label:         "for",
		Kind:          lsp.CompletionItemKindKeyword,
		Detail:        "loop",
		Documentation: "Creates a loop. Bolt uses `for` for all loop forms.",
	},
	{
		Label:         "range",
		Kind:          lsp.CompletionItemKindKeyword,
		Detail:        "range expression",
		Documentation: "Iterates over a value using a `for` loop.",
	},
	{
		Label:         "return",
		Kind:          lsp.CompletionItemKindKeyword,
		Detail:        "return statement",
		Documentation: "Returns a value from a function.",
	},
	{
		Label:         "break",
		Kind:          lsp.CompletionItemKindKeyword,
		Detail:        "loop control",
		Documentation: "Exits the current loop.",
	},
	{
		Label:         "continue",
		Kind:          lsp.CompletionItemKindKeyword,
		Detail:        "loop control",
		Documentation: "Skips to the next iteration of the current loop.",
	},
	{
		Label:         "import",
		Kind:          lsp.CompletionItemKindKeyword,
		Detail:        "module import",
		Documentation: "Imports a Bolt module.",
	},
	{
		Label:         "switch",
		Kind:          lsp.CompletionItemKindKeyword,
		Detail:        "switch expression",
		Documentation: "Selects a branch based on an expression.",
	},
	{
		Label:         "case",
		Kind:          lsp.CompletionItemKindKeyword,
		Detail:        "switch case",
		Documentation: "Defines a case inside a switch expression.",
	},
	{
		Label:         "defer",
		Kind:          lsp.CompletionItemKindKeyword,
		Detail:        "deferred expression",
		Documentation: "Defers an expression until the surrounding function returns.",
	},

	// Literals
	{
		Label:         "true",
		Kind:          lsp.CompletionItemKindValue,
		Detail:        "boolean",
		Documentation: "Boolean value representing true.",
	},
	{
		Label:         "false",
		Kind:          lsp.CompletionItemKindValue,
		Detail:        "boolean",
		Documentation: "Boolean value representing false.",
	},
	{
		Label:         "null",
		Kind:          lsp.CompletionItemKindValue,
		Detail:        "null value",
		Documentation: "Represents the absence of a value.",
	},

	// Built-in functions
	{
		Label:         "log",
		Kind:          lsp.CompletionItemKindFunction,
		Detail:        "fn(value)",
		Documentation: "Prints a value to standard output.",
	},
}

func (h *Handler) Completion(
	_ context.Context,
	params *lsp.CompletionParams,
) (*lsp.CompletionList, error) {

	items := make([]lsp.CompletionItem, 0, len(completions))

	for _, completion := range completions {
		item := lsp.CompletionItem{
			Label:  completion.Label,
			Kind:   &completion.Kind,
			Detail: completion.Detail,
			Documentation: &lsp.MarkupContent{
				Kind:  lsp.Markdown,
				Value: completion.Documentation,
			},
		}

		items = append(items, item)
	}

	return &lsp.CompletionList{
		IsIncomplete: false,
		Items:        items,
	}, nil
}

// Handler implements the LSP handler interfaces for bolt.
type Handler struct {
	documents *document.Store
	client    *server.Client
}

// New creates a new Handler.
func New() *Handler {
	return &Handler{
		documents: document.NewStore(),
	}
}

func (h *Handler) SetClient(client *server.Client) {
	h.client = client
}

func (h *Handler) Initialize(_ context.Context, _ *lsp.InitializeParams) (*lsp.InitializeResult, error) {
	return &lsp.InitializeResult{
		Capabilities: lsp.ServerCapabilities{
			CompletionProvider: &lsp.CompletionOptions{
				ResolveProvider: boolPtr(true),
			},
		},
		ServerInfo: &lsp.ServerInfo{Name: "bolt", Version: "0.1.0"},
	}, nil
}

func boolPtr(b bool) *bool                          { return &b }
func (h *Handler) Shutdown(_ context.Context) error { return nil }

func (h *Handler) DidOpen(ctx context.Context, params *lsp.DidOpenTextDocumentParams) error {
	_, err := h.documents.Open(params)
	if err != nil {
		return err
	}
	text, ok := h.documents.Text(params.TextDocument.URI)
	if ok {
		logging.Logger.Println("OpenedL ", params.TextDocument.URI)
		errs := h.RunDiagnostics(ctx, text, string(params.TextDocument.URI), true)
		return h.client.PublishDiagnostics(ctx, &lsp.PublishDiagnosticsParams{
			URI:         params.TextDocument.URI,
			Diagnostics: errs,
		})
	} else {
		return nil
	}
}

func (h *Handler) DidChange(ctx context.Context, params *lsp.DidChangeTextDocumentParams) error {
	_, err := h.documents.Change(params)
	if err == nil {
		text, ok := h.documents.Text(params.TextDocument.URI)
		logging.Logger.Println("Changed: ", params.TextDocument.URI)

		if ok {
			errs := h.RunDiagnostics(ctx, text, string(params.TextDocument.URI), true)
			return h.client.PublishDiagnostics(ctx, &lsp.PublishDiagnosticsParams{
				URI:         params.TextDocument.URI,
				Diagnostics: errs,
			})
		}
	}
	return err
}

func (h *Handler) DidClose(_ context.Context, params *lsp.DidCloseTextDocumentParams) error {
	h.documents.Close(params)
	return nil
}

func (h *Handler) DidSave(ctx context.Context, params *lsp.DidSaveTextDocumentParams) error {
	var diags []lsp.Diagnostic = []lsp.Diagnostic{}

	text, ok := h.documents.Text(params.TextDocument.URI)
	if ok {
		for i, line := range strings.Split(text, "\n") {
			idx := strings.Index(line, "TODO")
			if idx < 0 {
				continue
			}

			sev := lsp.SeverityWarning

			diags = append(diags, lsp.Diagnostic{
				Range: lsp.Range{
					Start: lsp.Position{
						Line:      i,
						Character: idx,
					},
					End: lsp.Position{
						Line:      i,
						Character: idx + 4,
					},
				},
				Severity: &sev,
				Source:   "bolt",
				Message:  "TODO found",
			})
		}

		logging.Logger.Println("Saved")

		errs := h.RunDiagnostics(ctx, text, string(params.TextDocument.URI), true)
		if len(errs) > 0 {
			diags = append(diags, errs...)
		}
	}

	return h.client.PublishDiagnostics(ctx, &lsp.PublishDiagnosticsParams{
		URI:         params.TextDocument.URI,
		Diagnostics: diags,
	})
}

func (h *Handler) Hover(_ context.Context, _ *lsp.HoverParams) (*lsp.Hover, error) {
	return &lsp.Hover{
		Contents: lsp.NewHoverContents(lsp.Markdown, "**bolt** hover"),
	}, nil
}

func (h *Handler) RunDiagnostics(ctx context.Context, text string, filename string, changed bool) []lsp.Diagnostic {
	// Parse the file to get any parser errors
	p, errs := analyzer.Parse(text)
	logging.Logger.Println("\n", "Parser errors: ", errs, "\n")
	if p != nil {
		// turn the URI to a filepath e.g. file:///home/sam/code/bolt/examples/functions.bolt -> /home/sam/code/bolt/examples/functions.bolt
		path := filename
		if strings.HasPrefix(filename, "file:///") {
			path = filename[7:]
		}
		path, _ = analyzer.CanonicalPath(path)

		// Get any identifier errors (Identifier not found)
		identifiererrs := analyzer.CheckInitsAndIdents(p, strings.Split(text, "\n"), path)
		logging.Logger.Println("Identifier errors: ", identifiererrs)
		if len(identifiererrs) > 0 {
			errs = append(errs, identifiererrs...)
		}

		// If there are no errors in the current code, we can update module cache.
		if len(errs) == 0 {
			// If the current file is a module (a dependency of another file) then continue
			if _, ok := analyzer.Modulemap[path]; ok {
				var program *ast.Program
				var name string

				// If it is not standard lib then we need to process the module. We cannot use readfile since that would get it from disk and when you are editing, it isn't saved to disk yet so we use the current text form the document we have
				if !strings.HasSuffix(path, ".bolt") && std.IsStandardLib(path) {
					program, name, _ = analyzer.GetImport(path, -1)
				} else {
					fpath, _ := os.Stat(path)
					name = utils.FileName(fpath.Name())
					program, _ = utils.ProcessModule(text, false)
				}
				logging.Logger.Println("Updating module cache for: ", name)
				if program != nil {
					exports := []string{}
					// Run through each statement and find any `pub` statements and add these to a list of exports
					for _, stmt := range program.Statements {
						if !analyzer.IsNil(stmt) {
							analyzer.WalkImport(stmt, analyzer.NewScope(), name, false, &exports)
						}
					}
					logging.Logger.Println("New module exports: ", exports)
					// Update the module cache
					analyzer.Modulemap[path] = &analyzer.Module{Name: path, Exports: exports}

					// If we are currently in the file that has been changed (the dependency file) then we need to update all files that depend on this file
					if changed {
						logging.Logger.Println(analyzer.ModuleDependencies, path)

						// go through all open documents and their dependencies, if we find a match then we get that document and run diagnostics on it to sync it up with the new changes to its dependency
						for file, dependencies := range analyzer.ModuleDependencies {
							if _, ok := dependencies[path]; ok {
								document, fine := h.documents.Get(lsp.DocumentURI("file://" + file))
								if fine == true {
									text := document.Text()
									d := h.RunDiagnostics(ctx, text, "file://"+file, false)
									h.client.PublishDiagnostics(ctx, &lsp.PublishDiagnosticsParams{
										URI:         lsp.DocumentURI("file://" + file),
										Diagnostics: d,
									})
								}
							}
						}
					}
				}
			}
		}
	}
	logging.Logger.Println("---------\n")

	return errs
}
