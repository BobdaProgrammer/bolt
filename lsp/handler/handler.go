package handler

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/BobdaProgrammer/bolt/ast"
	"github.com/BobdaProgrammer/bolt/lsp/analyzer"
	"github.com/BobdaProgrammer/bolt/lsp/hover"
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
			DefinitionProvider: boolPtr(true),
		},
		ServerInfo: &lsp.ServerInfo{Name: "bolt", Version: "0.1.0"},
	}, nil
}

func boolPtr(b bool) *bool                          { return &b }
func (h *Handler) Shutdown(_ context.Context) error { return nil }

// Specifically for go to definition
func walkForIdent(n ast.Node, line, column int) ast.Node {
	w := func(node ast.Node) ast.Node {
		return walkForIdent(node, line, column)
	}

	switch n := n.(type) {
	case *ast.Identifier:
		start := n.Token.Column - (len(n.Token.Literal) - 1)
		if n.Line() == line && column >= start && column <= n.Token.Column {
			return n
		}

	case *ast.LetStatement:
		// No left side since that is the definition
		if r := w(n.Value); r != nil {
			return r
		}
	case *ast.AssignExpression:
		if r := w(n.Left); r != nil {
			return r
		}
		if r := w(n.Value); r != nil {
			return r
		}
	case *ast.ReturnStatement:
		for _, rv := range n.ReturnValues {
			if r := w(rv); r != nil {
				return r
			}
		}
	case *ast.ExpressionStatement:
		if r := w(n.Expression); r != nil {
			return r
		}
	case *ast.PrefixExpression:
		if r := w(n.Right); r != nil {
			return r
		}
	case *ast.InfixExpression:
		if r := w(n.Left); r != nil {
			return r
		}
		if r := w(n.Right); r != nil {
			return r
		}
	case *ast.IfExpression:
		if r := w(n.Condition); r != nil {
			return r
		}
		if r := w(n.Consequence); r != nil {
			return r
		}
		if n.Alternative != nil {
			if r := w(n.Alternative); r != nil {
				return r
			}
		}
	case *ast.BlockStatement:
		for _, stmt := range n.Statements {
			if r := w(stmt); r != nil {
				return r
			}
		}
	case *ast.FunctionLiteral:
		for _, param := range n.Parameters {
			if r := w(param); r != nil {
				return r
			}
		}
		if r := w(n.Body); r != nil {
			return r
		}
	case *ast.CallExpression:
		for _, arg := range n.Arguments {
			if r := w(arg); r != nil {
				return r
			}
		}
		if r := w(n.Function); r != nil {
			return r
		}
	case *ast.ArrayLiteral:
		for _, el := range n.Elements {
			if r := w(el); r != nil {
				return r
			}
		}
	case *ast.IndexExpression:
		if r := w(n.Left); r != nil {
			return r
		}
		if r := w(n.Index); r != nil {
			return r
		}
	case *ast.IndexAssignExpression:
		if r := w(n.Left); r != nil {
			return r
		}
		if r := w(n.Index); r != nil {
			return r
		}
		if r := w(n.Value); r != nil {
			return r
		}
	case *ast.SliceExpression:

		if r := w(n.Left); r != nil {
			return r
		}
		if r := w(n.IndexStart); r != nil {
			return r
		}
		if r := w(n.IndexEnd); r != nil {
			return r
		}
	case *ast.HashLiteral:
		for k, v := range n.Pairs {
			if r := w(k); r != nil {
				return r
			}
			if r := w(v); r != nil {
				return r
			}
		}
	case *ast.ForExpression:
		if r := w(n.Condition); r != nil {
			return r
		}
		if r := w(n.Consequence); r != nil {
			return r
		}
	case *ast.RangeExpression:
		if r := w(n.Val1); r != nil {
			return r
		}
		if n.Val2 != nil {
			if r := w(n.Val2); r != nil {
				return r
			}
		}
		if r := w(n.Ranging); r != nil {
			return r
		}
	case *ast.StructInstantiation:
		if r := w(n.Left); r != nil {
			return r
		}
		for _, field := range n.Fields {
			if r := w(field); r != nil {
				return r
			}
		}
	case *ast.FieldAccess:
		if r := w(n.Left); r != nil {
			return r
		}
		if r := w(n.Right); r != nil {
			return r
		}
	case *ast.PubStatement:
		if r := w(n.Stmt); r != nil {
			return r
		}
	case *ast.StructTypeConversion:
		if r := w(n.Left); r != nil {
			return r
		}
		if r := w(n.StType); r != nil {
			return r
		}
	case *ast.SwitchExpression:
		if r := w(n.SwitchItem); r != nil {
			return r
		}
		for _, cas := range n.Cases {
			if r := w(cas); r != nil {
				return r
			}
		}
	case *ast.Defer:
		if r := w(n.Exp); r != nil {
			return r
		}
	default:
		return nil
	}
	return nil
}

func (h *Handler) Definition(ctx context.Context, params *lsp.DefinitionParams) ([]lsp.Location, error) {
	// document, fine := h.documents.Get(params.TextDocument.URI)
	// if !fine {
	// 	return []lsp.Location{}, nil
	// }

	logging.Logger.Println("GOING TO DEFINITION")

	identlines, ok := analyzer.DocToIdentLines[params.TextDocument.URI]

	if ok {

		logging.Logger.Println("Looking for ident with Line:", params.Position.Line, "Character:", params.Position.Character)
		for _, LocInit := range identlines.Idents {
			logging.Logger.Println(LocInit.Pos.Line-1, LocInit.Pos.StartColumn-1, LocInit.Pos.EndColumn-1)
			if params.Position.Line == LocInit.Pos.Line-1 && params.Position.Character >= LocInit.Pos.StartColumn-1 && params.Position.Character <= LocInit.Pos.EndColumn-1 {
				uri := params.TextDocument.URI
				if LocInit.Init.ImportFile != "" {
					uri = lsp.DocumentURI("file://" + LocInit.Init.ImportFile)
				}
				return []lsp.Location{
					lsp.Location{
						URI: uri,
						Range: lsp.Range{
							Start: lsp.Position{
								Line:      LocInit.Init.Line - 1,
								Character: LocInit.Init.Column - 1,
							},
							End: lsp.Position{
								Line:      LocInit.Init.Line - 1,
								Character: LocInit.Init.Column - 1, //len(document.Lines()[LocInit.Init.Line-1]) - 1,
							},
						},
					},
				}, nil
			}
		}

	}

	return []lsp.Location{
		lsp.Location{
			URI: params.TextDocument.URI,
			Range: lsp.Range{
				Start: lsp.Position{
					Line:      params.Position.Line,
					Character: params.Position.Character,
				},
				End: lsp.Position{
					Line:      params.Position.Line,
					Character: params.Position.Character,
				},
			},
		},
	}, nil
}

func (h *Handler) DidOpen(ctx context.Context, params *lsp.DidOpenTextDocumentParams) error {
	_, err := h.documents.Open(params)
	if err != nil {
		return err
	}
	text, ok := h.documents.Text(params.TextDocument.URI)
	if ok {
		logging.Logger.Println("OpenedL ", params.TextDocument.URI)
		errs := h.RunDiagnostics(ctx, text, string(params.TextDocument.URI), params.TextDocument.URI, true)
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
			errs := h.RunDiagnostics(ctx, text, string(params.TextDocument.URI), params.TextDocument.URI, true)
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
		logging.Logger.Println("Saved")

		errs := h.RunDiagnostics(ctx, text, string(params.TextDocument.URI), params.TextDocument.URI, true)
		if len(errs) > 0 {
			diags = append(diags, errs...)
		}
	}

	return h.client.PublishDiagnostics(ctx, &lsp.PublishDiagnosticsParams{
		URI:         params.TextDocument.URI,
		Diagnostics: diags,
	})
}

func (h *Handler) Hover(_ context.Context, params *lsp.HoverParams) (*lsp.Hover, error) {
	identlines, ok := analyzer.DocToIdentLines[params.TextDocument.URI]
	hov := ""
	if ok {
		for _, LocInit := range identlines.Idents {
			logging.Logger.Println(LocInit.Pos.Line-1, LocInit.Pos.StartColumn-1, LocInit.Pos.EndColumn-1)
			if params.Position.Line == LocInit.Pos.Line-1 && params.Position.Character >= LocInit.Pos.StartColumn-1 && params.Position.Character <= LocInit.Pos.EndColumn-1 {
				if LocInit.Init.Kind != analyzer.UNKNOWN {
					hov += "Type: " + analyzer.InitTypeToString[LocInit.Init.Kind] + "\n"
				}
				if LocInit.Init.NodeCopy != nil {
					logging.Logger.Println(hover.Documentation(LocInit.Init.NodeCopy, LocInit.Init, LocInit.Name, LocInit.Scope))
					if doc := hover.Documentation(LocInit.Init.NodeCopy, LocInit.Init, LocInit.Name, LocInit.Scope); doc != "" { // TODO: give it letident instead of nil
						hov = doc
					} else {
						logging.Logger.Println("Documentation node copy: ", LocInit.Init.NodeCopy.String())
						hov += "```bolt\n" + LocInit.Init.NodeCopy.String() + "\n```"
					}
				}
			}
		}
	}
	return &lsp.Hover{
		Contents: lsp.NewHoverContents(lsp.Markdown, hov),
	}, nil
}

func isWithinDir(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}

	return rel != ".." &&
		!strings.HasPrefix(rel, ".."+string(filepath.Separator)) &&
		!filepath.IsAbs(rel)
}

func (h *Handler) RunDiagnostics(ctx context.Context, text string, filename string, URI lsp.DocumentURI, changed bool) []lsp.Diagnostic {
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

		cacheDir, err := os.UserCacheDir()
		isStd := false
		if err == nil {
			destDir := filepath.Join(cacheDir, "bolt", "stdlib")
			if isWithinDir(destDir, path) {
				isStd = true
			}
		}

		// Get any identifier errors (Identifier not found)
		identifiererrs := analyzer.CheckInitsAndIdents(p, strings.Split(text, "\n"), path, URI, isStd)
		logging.Logger.Println("Identifier errors: ", identifiererrs)
		if len(identifiererrs) > 0 {
			errs = append(errs, identifiererrs...)
		}

		// If there are no errors in the current code, we can update module cache.
		if len(errs) == 0 {
			// If the current file is a module (a dependency of another file) then continue
			logging.Logger.Println("Module: ", path, analyzer.Modulemap[path])
			if _, ok := analyzer.Modulemap[path]; ok {
				var program *ast.Program
				var name string

				// If it is not standard lib then we need to process the module. We cannot use readfile since that would get it from disk and when you are editing, it isn't saved to disk yet so we use the current text form the document we have
				if !strings.HasSuffix(path, ".bolt") && std.IsStandardLib(path) {
					program, name, _, _, _ = analyzer.GetImport(path, -1)
				} else {
					fpath, _ := os.Stat(path)
					name = utils.FileName(fpath.Name())
					program, _ = utils.ProcessModule(text, false)
				}
				logging.Logger.Println("Updating module cache for: ", name)
				if program != nil {
					logging.Logger.Println("New module exports")
					exports := analyzer.ProcessImport(program, name, path)
					// Update the module cache
					analyzer.Modulemap[path] = &analyzer.Module{Name: path, Exports: exports}

					// If we are currently in the file that has been changed (the dependency file) then we need to update all files that depend on this file
					logging.Logger.Println("Checking dependencies")
					if changed {
						logging.Logger.Println("Module dependencies:", analyzer.ModuleDependencies, path)

						// go through all open documents and their dependencies, if we find a match then we get that document and run diagnostics on it to sync it up with the new changes to its dependency
						for file, dependencies := range analyzer.ModuleDependencies {
							logging.Logger.Println("Dependency:", file, dependencies)
							if _, ok := dependencies[path]; ok {
								logging.Logger.Println("Found dependency: ", path)
								document, fine := h.documents.Get(lsp.DocumentURI("file://" + file))
								if fine == true {
									text := document.Text()
									d := h.RunDiagnostics(ctx, text, "file://"+file, document.URI(), false)
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
