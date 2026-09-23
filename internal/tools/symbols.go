package tools

import (
	"bufio"
	"bytes"
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/olostan/DevCadence/internal/errs"
)

// ResolutionLevel represents the semantic depth of symbol resolution (ADR-0016).
type ResolutionLevel string

const (
	ResolutionSyntactic       ResolutionLevel = "syntactic"
	ResolutionUnsupported     ResolutionLevel = "unsupported"
	ResolutionPartiallyParsed ResolutionLevel = "partially_parsed"
)

// SymbolDefinition represents an AST definition of a symbol.
type SymbolDefinition struct {
	Path     string `json:"path"`
	Line     int    `json:"line"`
	Kind     string `json:"kind"` // "function_declaration", "method_declaration", "type_spec", "class_declaration", "interface_declaration"
	Name     string `json:"name"`
	Receiver string `json:"receiver,omitempty"`
}

// SymbolCall represents an AST candidate call expression targeting the symbol.
type SymbolCall struct {
	Path   string `json:"path"`
	Line   int    `json:"line"`
	Caller string `json:"caller,omitempty"`
	Name   string `json:"name"`
}

// SymbolResult holds syntactic symbol match outcomes.
type SymbolResult struct {
	Symbol          string             `json:"symbol"`
	ResolutionLevel ResolutionLevel    `json:"resolution_level"`
	Backend         string             `json:"backend"`
	Language        string             `json:"language"`
	SourceRevision  string             `json:"source_revision,omitempty"`
	Definitions     []SymbolDefinition `json:"definitions"`
	CandidateCalls  []SymbolCall       `json:"candidate_calls"`
	FallbackQuery   string             `json:"fallback_query,omitempty"`
}

// FindSymbolOptions configures syntactic symbol inspection.
type FindSymbolOptions struct {
	Scope          Scope
	Symbol         string
	Path           string // Relative to scope
	Language       string // "go", "typescript", "javascript" (or auto-detect)
	SourceRevision string
}

// FindSymbol searches for symbol definitions and call expressions syntactically (ADR-0016).
func FindSymbol(ctx context.Context, opts FindSymbolOptions) (SymbolResult, error) {
	if opts.Symbol == "" {
		return SymbolResult{}, errs.New(errs.CategoryInvalidArgument, "find_symbol: symbol name is required")
	}

	targetPath, err := opts.Scope.ResolvePath(opts.Path)
	if err != nil {
		return SymbolResult{}, err
	}

	resolvedWorktree, err := filepath.EvalSymlinks(opts.Scope.WorktreePath)
	if err != nil {
		return SymbolResult{}, errs.Wrap(errs.CategoryInvalidArgument, err, "find_symbol: resolve worktree %q", opts.Scope.WorktreePath)
	}

	stat, err := os.Stat(targetPath)
	if err != nil {
		return SymbolResult{}, errs.Wrap(errs.CategoryNotFound, err, "find_symbol: stat %q", targetPath)
	}

	// Detect language if not explicitly specified
	lang := strings.ToLower(opts.Language)
	if lang == "" && !stat.IsDir() {
		lang = detectLanguage(targetPath)
	}

	// If language is known and unsupported
	if lang != "" && lang != "go" && lang != "typescript" && lang != "javascript" {
		return SymbolResult{
			Symbol:          opts.Symbol,
			ResolutionLevel: ResolutionUnsupported,
			Backend:         "none",
			Language:        lang,
			SourceRevision:  opts.SourceRevision,
			FallbackQuery:   opts.Symbol,
		}, nil
	}

	result := SymbolResult{
		Symbol:          opts.Symbol,
		ResolutionLevel: ResolutionSyntactic,
		Backend:         "",
		Language:        lang,
		SourceRevision:  opts.SourceRevision,
		Definitions:     []SymbolDefinition{},
		CandidateCalls:  []SymbolCall{},
	}

	walkFn := func(path string, d fs.DirEntry, err error) error {
		if err != nil || ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "node_modules" || name == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}

		fileLang := detectLanguage(path)
		if lang != "" && fileLang != lang {
			return nil
		}

		relPath, err := filepath.Rel(resolvedWorktree, path)
		if err != nil {
			return nil
		}

		switch fileLang {
		case "go":
			if result.Language == "" {
				result.Language = "go"
			}
			result.Backend = "go/ast"
			parseGoFile(path, relPath, opts.Symbol, &result)
		case "typescript", "javascript":
			if result.Language == "" {
				result.Language = fileLang
			}
			result.Backend = "syntactic-regex"
			parseTSFile(path, relPath, opts.Symbol, &result)
		default:
			// Non-supported file in directory walk
		}

		return nil
	}

	if stat.IsDir() {
		_ = filepath.WalkDir(targetPath, walkFn)
	} else {
		_ = walkFn(targetPath, fs.FileInfoToDirEntry(stat), nil)
	}

	return result, nil
}

func detectLanguage(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".go":
		return "go"
	case ".ts", ".tsx":
		return "typescript"
	case ".js", ".jsx", ".mjs", ".cjs":
		return "javascript"
	default:
		return ""
	}
}

func parseGoFile(fullPath, relPath, targetSymbol string, result *SymbolResult) {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, fullPath, nil, 0)
	if err != nil {
		result.ResolutionLevel = ResolutionPartiallyParsed
		return
	}

	ast.Inspect(node, func(n ast.Node) bool {
		if n == nil {
			return true
		}

		switch x := n.(type) {
		case *ast.FuncDecl:
			line := fset.Position(x.Pos()).Line
			if x.Name.Name == targetSymbol {
				if x.Recv == nil {
					result.Definitions = append(result.Definitions, SymbolDefinition{
						Path: relPath,
						Line: line,
						Kind: "function_declaration",
						Name: x.Name.Name,
					})
				} else {
					receiverType := ""
					if len(x.Recv.List) > 0 {
						receiverType = fmtReceiver(x.Recv.List[0].Type)
					}
					result.Definitions = append(result.Definitions, SymbolDefinition{
						Path:     relPath,
						Line:     line,
						Kind:     "method_declaration",
						Name:     x.Name.Name,
						Receiver: receiverType,
					})
				}
			}

		case *ast.TypeSpec:
			line := fset.Position(x.Pos()).Line
			if x.Name.Name == targetSymbol {
				result.Definitions = append(result.Definitions, SymbolDefinition{
					Path: relPath,
					Line: line,
					Kind: "type_spec",
					Name: x.Name.Name,
				})
			}

		case *ast.CallExpr:
			line := fset.Position(x.Pos()).Line
			callName, caller := extractCallName(x.Fun)
			if callName == targetSymbol {
				result.CandidateCalls = append(result.CandidateCalls, SymbolCall{
					Path:   relPath,
					Line:   line,
					Caller: caller,
					Name:   callName,
				})
			}
		}

		return true
	})
}

func fmtReceiver(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.StarExpr:
		return "*" + fmtReceiver(t.X)
	case *ast.Ident:
		return t.Name
	case *ast.IndexExpr:
		return fmtReceiver(t.X)
	default:
		return ""
	}
}

func extractCallName(fun ast.Expr) (string, string) {
	switch x := fun.(type) {
	case *ast.Ident:
		return x.Name, ""
	case *ast.SelectorExpr:
		callerName := ""
		if id, ok := x.X.(*ast.Ident); ok {
			callerName = id.Name
		}
		return x.Sel.Name, callerName
	default:
		return "", ""
	}
}

var (
	tsFuncDeclRegex   = regexp.MustCompile(`(?:export\s+)?(?:async\s+)?function\s+([A-Za-z0-9_$]+)`)
	tsClassDeclRegex  = regexp.MustCompile(`(?:export\s+)?class\s+([A-Za-z0-9_$]+)`)
	tsInterfaceRegex  = regexp.MustCompile(`(?:export\s+)?interface\s+([A-Za-z0-9_$]+)`)
	tsTypeRegex       = regexp.MustCompile(`(?:export\s+)?type\s+([A-Za-z0-9_$]+)\s*=`)
	tsMethodRegex     = regexp.MustCompile(`^\s*(?:async\s+)?([A-Za-z0-9_$]+)\s*\([^)]*\)\s*[{:]`)
	tsArrowFuncRegex  = regexp.MustCompile(`(?:const|let|var)\s+([A-Za-z0-9_$]+)\s*=\s*(?:async\s*)?(?:\([^)]*\)|[A-Za-z0-9_$]+)\s*=>`)
	tsCallExprRegex   = regexp.MustCompile(`(?:([A-Za-z0-9_$]+)\.)?([A-Za-z0-9_$]+)\s*\(`)
)

func parseTSFile(fullPath, relPath, targetSymbol string, result *SymbolResult) {
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return
	}

	masked := maskCommentsAndStrings(data)
	scanner := bufio.NewScanner(bytes.NewReader(masked))
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		// 1. Function declaration
		if m := tsFuncDeclRegex.FindStringSubmatch(line); len(m) > 1 && m[1] == targetSymbol {
			result.Definitions = append(result.Definitions, SymbolDefinition{
				Path: relPath,
				Line: lineNum,
				Kind: "function_declaration",
				Name: m[1],
			})
			continue
		}

		// 2. Arrow function
		if m := tsArrowFuncRegex.FindStringSubmatch(line); len(m) > 1 && m[1] == targetSymbol {
			result.Definitions = append(result.Definitions, SymbolDefinition{
				Path: relPath,
				Line: lineNum,
				Kind: "function_declaration",
				Name: m[1],
			})
			continue
		}

		// 3. Class declaration
		if m := tsClassDeclRegex.FindStringSubmatch(line); len(m) > 1 && m[1] == targetSymbol {
			result.Definitions = append(result.Definitions, SymbolDefinition{
				Path: relPath,
				Line: lineNum,
				Kind: "class_declaration",
				Name: m[1],
			})
			continue
		}

		// 4. Interface declaration
		if m := tsInterfaceRegex.FindStringSubmatch(line); len(m) > 1 && m[1] == targetSymbol {
			result.Definitions = append(result.Definitions, SymbolDefinition{
				Path: relPath,
				Line: lineNum,
				Kind: "interface_declaration",
				Name: m[1],
			})
			continue
		}

		// 5. Type alias
		if m := tsTypeRegex.FindStringSubmatch(line); len(m) > 1 && m[1] == targetSymbol {
			result.Definitions = append(result.Definitions, SymbolDefinition{
				Path: relPath,
				Line: lineNum,
				Kind: "type_spec",
				Name: m[1],
			})
			continue
		}

		// 6. Method declaration
		if m := tsMethodRegex.FindStringSubmatch(line); len(m) > 1 && m[1] == targetSymbol {
			// Avoid matching if/while/for
			if m[1] != "if" && m[1] != "while" && m[1] != "for" && m[1] != "switch" {
				result.Definitions = append(result.Definitions, SymbolDefinition{
					Path: relPath,
					Line: lineNum,
					Kind: "method_declaration",
					Name: m[1],
				})
			}
		}

		// 7. Call expressions
		matches := tsCallExprRegex.FindAllStringSubmatch(line, -1)
		for _, match := range matches {
			if len(match) > 2 && match[2] == targetSymbol {
				caller := match[1]
				result.CandidateCalls = append(result.CandidateCalls, SymbolCall{
					Path:   relPath,
					Line:   lineNum,
					Caller: caller,
					Name:   targetSymbol,
				})
			}
		}
	}
}

// maskCommentsAndStrings masks single-line/multi-line comments and string literals
// with spaces while preserving newlines and positions to avoid regex false positives.
func maskCommentsAndStrings(src []byte) []byte {
	out := make([]byte, len(src))
	copy(out, src)

	n := len(out)
	i := 0
	for i < n {
		b := out[i]
		if b == '/' && i+1 < n {
			next := out[i+1]
			if next == '/' {
				// Single line comment
				out[i] = ' '
				out[i+1] = ' '
				i += 2
				for i < n && out[i] != '\n' {
					out[i] = ' '
					i++
				}
				continue
			} else if next == '*' {
				// Multi-line block comment
				out[i] = ' '
				out[i+1] = ' '
				i += 2
				for i < n {
					if out[i] == '*' && i+1 < n && out[i+1] == '/' {
						out[i] = ' '
						out[i+1] = ' '
						i += 2
						break
					}
					if out[i] != '\n' {
						out[i] = ' '
					}
					i++
				}
				continue
			}
		}

		if b == '"' || b == '\'' || b == '`' {
			quote := b
			out[i] = ' '
			i++
			escaped := false
			for i < n {
				if escaped {
					if out[i] != '\n' {
						out[i] = ' '
					}
					escaped = false
					i++
					continue
				}
				if out[i] == '\\' {
					escaped = true
					out[i] = ' '
					i++
					continue
				}
				if out[i] == quote {
					out[i] = ' '
					i++
					break
				}
				if quote != '`' && out[i] == '\n' {
					// Single-line string terminated by newline
					break
				}
				if out[i] != '\n' {
					out[i] = ' '
				}
				i++
			}
			continue
		}

		i++
	}
	return out
}
