package main

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Decl struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	StartLine int    `json:"start"`
	EndLine   int    `json:"end"`
	Target    string `json:"target,omitempty"`
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: splitter [-split] <file.go>\n")
		os.Exit(1)
	}
	split := false
	args := os.Args[1:]
	if args[0] == "-split" {
		split = true
		args = args[1:]
	}
	if len(args) != 1 {
		fmt.Fprintf(os.Stderr, "Usage: splitter [-split] <file.go>\n")
		os.Exit(1)
	}
	filePath := args[0]

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
	if err != nil {
		fmt.Fprintf(os.Stderr, "parse error: %v\n", err)
		os.Exit(1)
	}

	var decls []Decl
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			kind := "func"
			recv := ""
			if d.Recv != nil && len(d.Recv.List) > 0 {
				kind = "method"
				switch t := d.Recv.List[0].Type.(type) {
				case *ast.StarExpr:
					if ident, ok := t.X.(*ast.Ident); ok {
						recv = "*" + ident.Name
					}
				case *ast.Ident:
					recv = t.Name
				}
			}
			name := d.Name.Name
			if recv != "" {
				name = recv + "." + name
			}
			start := fset.Position(d.Pos()).Line
			if d.Doc != nil {
				start = fset.Position(d.Doc.Pos()).Line
			}
			end := fset.Position(d.End()).Line
			decls = append(decls, Decl{Name: name, Kind: kind, StartLine: start, EndLine: end, Target: targetForDecl(name, start)})

		case *ast.GenDecl:
			var kind string
			switch d.Tok {
			case token.TYPE:
				kind = "type"
			case token.CONST:
				kind = "const"
			case token.VAR:
				kind = "var"
			default:
				kind = d.Tok.String()
			}
			start := fset.Position(d.Pos()).Line
			if d.Doc != nil {
				start = fset.Position(d.Doc.Pos()).Line
			}
			end := fset.Position(d.End()).Line
			var names []string
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					names = append(names, s.Name.Name)
				case *ast.ValueSpec:
					for _, n := range s.Names {
						names = append(names, n.Name)
					}
				}
			}
			name := strings.Join(names, ", ")
			if len(names) == 0 {
				name = fmt.Sprintf("%s-block", kind)
			}
			decls = append(decls, Decl{Name: name, Kind: kind, StartLine: start, EndLine: end, Target: targetForDecl(name, start)})
		}
	}

	sort.Slice(decls, func(i, j int) bool {
		return decls[i].StartLine < decls[j].StartLine
	})

	if split {
		if err := splitServiceFile(filePath, decls); err != nil {
			fmt.Fprintf(os.Stderr, "split error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	out, _ := json.MarshalIndent(decls, "", "  ")
	fmt.Print(string(out))
}

func targetForDecl(name string, start int) string {
	switch {
	case start >= 566 && start <= 1199:
		return "service_files.go"
	case start >= 121 && start <= 263, start >= 1201 && start <= 2029:
		return "service_terminal.go"
	case start >= 2031 && start <= 2338:
		return "service_approvals.go"
	case start >= 2340 && start <= 2704, start >= 3162 && start <= 3371, start >= 5562 && start <= 5731:
		return "service_agents.go"
	case start >= 2757 && start <= 3130:
		return "service_app.go"
	case start >= 3394 && start <= 3908:
		return "service_middleware.go"
	case start >= 3910 && start <= 4154:
		return "service_auth_handlers.go"
	case start >= 4156 && start <= 4304:
		return "service_api_misc.go"
	case start >= 4306 && start <= 4395, start >= 4683 && start <= 5081:
		return "service_configure.go"
	case start >= 4397 && start <= 4681:
		return "service_skills.go"
	case start >= 5083 && start <= 5320:
		return "service_models.go"
	case start >= 5322 && start <= 5560:
		return "service_cron.go"
	default:
		return "service.go"
	}
}

func splitServiceFile(filePath string, decls []Decl) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}
	lines := strings.SplitAfter(string(data), "\n")
	if len(lines) == 0 {
		return fmt.Errorf("empty file")
	}

	importEnd := 0
	for _, decl := range decls {
		if decl.Kind == "import" {
			importEnd = decl.EndLine
			break
		}
	}
	if importEnd == 0 || importEnd > len(lines) {
		return fmt.Errorf("could not locate import block")
	}
	header := strings.Join(lines[:importEnd], "") + "\n"

	byTarget := map[string][]Decl{}
	for _, decl := range decls {
		if decl.Kind == "import" {
			continue
		}
		byTarget[decl.Target] = append(byTarget[decl.Target], decl)
	}

	dir := filepath.Dir(filePath)
	for target, targetDecls := range byTarget {
		sort.Slice(targetDecls, func(i, j int) bool { return targetDecls[i].StartLine < targetDecls[j].StartLine })
		var builder strings.Builder
		builder.WriteString(header)
		for _, decl := range targetDecls {
			if decl.StartLine < 1 || decl.EndLine > len(lines) || decl.StartLine > decl.EndLine {
				return fmt.Errorf("invalid range for %s: %d-%d", decl.Name, decl.StartLine, decl.EndLine)
			}
			builder.WriteString(strings.Join(lines[decl.StartLine-1:decl.EndLine], ""))
			builder.WriteString("\n\n")
		}
		if err := os.WriteFile(filepath.Join(dir, target), []byte(builder.String()), 0o644); err != nil {
			return err
		}
	}
	return nil
}
