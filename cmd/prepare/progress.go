package main

import (
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
)

type sourceEdit struct {
	start, end int
	text       string
}

// Keep these hooks deterministic against the pinned source. Fail the build if
// upstream changes the async paths, rather than silently reporting completion.
func patchSearchProgress(root, source string) error {
	for _, file := range []string{"plugin/plugin.go", "service/search_service.go"} {
		original, err := exec.Command("git", "-C", source, "show", revision+":"+file).Output()
		if err != nil {
			return err
		}
		patched, err := instrumentProgress(file, original)
		if err != nil {
			return err
		}
		if err = os.WriteFile(filepath.Join(source, file), patched, 0644); err != nil {
			return err
		}
	}
	for _, pkg := range []string{"plugin", "service", "api"} {
		data, err := os.ReadFile(filepath.Join(root, "buildtools", "pansou-progress-"+pkg+".go.txt"))
		if err != nil {
			return err
		}
		data, err = format.Source(data)
		if err != nil {
			return err
		}
		if err = os.WriteFile(filepath.Join(source, pkg, "panreel_progress.go"), data, 0644); err != nil {
			return err
		}
	}
	test, err := os.ReadFile(filepath.Join(root, "buildtools", "pansou-progress-service_test.go.txt"))
	if err != nil {
		return err
	}
	test, err = format.Source(test)
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(source, "service", "panreel_progress_test.go"), test, 0644); err != nil {
		return err
	}
	return nil
}

func instrumentProgress(name string, data []byte) ([]byte, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, name, data, 0)
	if err != nil {
		return nil, err
	}
	var edits []sourceEdit
	add := func(pos token.Pos, text string) {
		offset := fset.Position(pos).Offset
		edits = append(edits, sourceEdit{offset, offset, text})
	}
	gos, batches, tasks := 0, 0, 0
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		key, begin, launch := "mainCacheKey", "DesktopBegin", "DesktopGo"
		if name == "plugin/plugin.go" {
			if fn.Name.Name != "AsyncSearch" && fn.Name.Name != "AsyncSearchWithResult" {
				continue
			}
		} else {
			if !slices.Contains([]string{"searchTG", "searchPlugins", "backfillTGChannels", "backfillPlugins", "writeSearchCacheByCompleteness"}, fn.Name.Name) {
				continue
			}
			key, begin, launch = "cacheKey", "desktopBeginWork", "plugin.DesktopGo"
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.GoStmt:
				start := fset.Position(node.Go).Offset
				edits = append(edits, sourceEdit{start, start + 3, launch + "(" + key + ", func() { "})
				add(node.End(), " })")
				gos++
			case *ast.AssignStmt:
				if name != "service/search_service.go" || node.Tok != token.DEFINE || len(node.Lhs) != 1 {
					break
				}
				if id, ok := node.Lhs[0].(*ast.Ident); ok && id.Name == "cacheKey" {
					add(node.End(), "\n\tdefer "+begin+"("+key+")()")
					batches++
				}
			case *ast.FuncLit:
				if name == "service/search_service.go" && len(node.Type.Params.List) == 1 && len(node.Type.Params.List[0].Names) == 1 && node.Type.Params.List[0].Names[0].Name == "ctx" {
					add(node.Body.Lbrace+1, "\nif ctx.Err() != nil { return nil }; defer "+begin+"("+key+")()\n")
					tasks++
				}
			}
			return true
		})
	}
	if (name == "plugin/plugin.go" && gos != 8) || (name == "service/search_service.go" && (gos != 3 || batches != 2 || tasks != 4)) {
		return nil, fmt.Errorf("upstream search progress patch does not apply to %s: goroutines=%d batches=%d tasks=%d", name, gos, batches, tasks)
	}
	slices.SortFunc(edits, func(a, b sourceEdit) int { return b.start - a.start })
	for _, edit := range edits {
		data = slices.Concat(data[:edit.start:edit.start], []byte(edit.text), data[edit.end:])
	}
	return format.Source(data)
}
