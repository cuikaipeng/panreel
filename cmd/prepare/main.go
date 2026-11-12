// prepare fetches the pinned upstream engine and packages a self-contained helper.
// End users never run this command or need a Go toolchain.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
)

var revision = "221665fb2c9279f22af4684ea2d8c0c331eac1cc"

type plugin struct {
	ID  string   `json:"id"`
	Env []string `json:"env,omitempty"`
}
type catalog struct {
	Revision string   `json:"revision"`
	Plugins  []plugin `json:"plugins"`
}

func main() {
	target := flag.String("platform", runtime.GOOS+"/"+runtime.GOARCH, "comma-separated darwin/arm64,darwin/amd64,windows/amd64")
	flag.StringVar(&revision, "revision", revision, "exact upstream commit for an independent engine release")
	flag.Parse()
	if !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(revision) {
		fmt.Fprintln(os.Stderr, "revision must be a full commit SHA")
		os.Exit(1)
	}
	if err := prepare(*target); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(dir string, env []string, command string, args ...string) error {
	c := exec.Command(command, args...)
	c.Dir = dir
	c.Env = env
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}
func prepare(targets string) error {
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	cache := filepath.Join(root, ".cache", "pansou")
	source := filepath.Join(cache, "source")
	if err = os.MkdirAll(cache, 0755); err != nil {
		return err
	}
	if _, err = os.Stat(filepath.Join(source, ".git")); os.IsNotExist(err) {
		if err = run(root, nil, "git", "init", source); err != nil {
			return err
		}
		if err = run(source, nil, "git", "remote", "add", "origin", "https://github.com/fish2018/pansou.git"); err != nil {
			return err
		}
	}
	if err = checkoutRevision(source, revision); err != nil {
		return err
	}
	main, err := exec.Command("git", "-C", source, "show", revision+":main.go").Output()
	if err != nil {
		return err
	}
	file, err := parser.ParseFile(token.NewFileSet(), "main.go", main, 0)
	if err != nil {
		return err
	}
	c := catalog{Revision: revision}
	for _, imp := range file.Imports {
		p, _ := strconv.Unquote(imp.Path.Value)
		if !strings.HasPrefix(p, "pansou/plugin/") {
			continue
		}
		id := strings.TrimPrefix(p, "pansou/plugin/")
		item := plugin{ID: id}
		files, _ := filepath.Glob(filepath.Join(source, "plugin", id, "*.go"))
		for _, path := range files {
			if strings.HasSuffix(path, "_test.go") {
				continue
			}
			f, e := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			if e != nil {
				return e
			}
			ast.Inspect(f, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok || len(call.Args) != 1 {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				pkg, ok := sel.X.(*ast.Ident)
				if !ok || pkg.Name != "os" || (sel.Sel.Name != "Getenv" && sel.Sel.Name != "LookupEnv") {
					return true
				}
				lit, ok := call.Args[0].(*ast.BasicLit)
				if !ok {
					return true
				}
				s, e := strconv.Unquote(lit.Value)
				if e == nil && !slices.Contains(item.Env, s) {
					item.Env = append(item.Env, s)
				}
				return true
			})
		}
		slices.Sort(item.Env)
		c.Plugins = append(c.Plugins, item)
	}
	slices.SortFunc(c.Plugins, func(a, b plugin) int { return strings.Compare(a.ID, b.ID) })
	// Channels are supplied only by the user; do not import upstream's
	// example channel list into the desktop catalog.
	if len(c.Plugins) < 20 {
		return fmt.Errorf("incomplete upstream catalog")
	}
	data, _ := json.MarshalIndent(c, "", "  ")
	data = append(data, '\n')
	if err = os.MkdirAll(filepath.Join(root, "internal", "catalog"), 0755); err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(root, "internal", "catalog", "catalog.json"), data, 0644); err != nil {
		return err
	}
	// All modifications are deterministic against the pinned original source.
	s := string(main)
	s = regexp.MustCompile(`Addr:\s*":" \+ port,`).ReplaceAllString(s, `Addr: "127.0.0.1:" + port,`)
	s = regexp.MustCompile(`Handler:\s*router,`).ReplaceAllString(s, `Handler: desktopHandler(router),`)
	s = strings.Replace(s, "signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)", "signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)\n\twatchDesktopLifetime(quit)", 1)
	if !strings.Contains(s, "desktopHandler(router)") || !strings.Contains(s, `"127.0.0.1:"`) {
		return fmt.Errorf("upstream desktop patch does not apply")
	}
	if err = os.WriteFile(filepath.Join(source, "main.go"), []byte(s), 0644); err != nil {
		return err
	}
	config, err := exec.Command("git", "-C", source, "show", revision+":config/config.go").Output()
	if err != nil {
		return err
	}
	original := "channelsEnv := os.Getenv(\"CHANNELS\")"
	replacement := original + "\n\tif os.Getenv(\"PANREEL_MANAGED\") == \"1\" && channelsEnv == \"\" { return []string{} }"
	if !strings.Contains(string(config), original) {
		return fmt.Errorf("upstream channel patch does not apply")
	}
	if err = os.WriteFile(filepath.Join(source, "config", "config.go"), []byte(strings.Replace(string(config), original, replacement, 1)), 0644); err != nil {
		return err
	}
	patch, err := os.ReadFile(filepath.Join(root, "buildtools", "pansou-desktop.go.txt"))
	if err != nil {
		return err
	}
	patch = []byte(strings.ReplaceAll(string(patch), "PANREEL_ENGINE_REVISION", revision))
	if err = os.WriteFile(filepath.Join(source, "panreel_desktop.go"), patch, 0644); err != nil {
		return err
	}
	if err = patchSearchProgress(root, source); err != nil {
		return err
	}
	for _, target := range strings.Split(targets, ",") {
		parts := strings.Split(target, "/")
		if len(parts) != 2 || (parts[0] != "darwin" && parts[0] != "windows") || (parts[1] != "arm64" && parts[1] != "amd64") {
			return fmt.Errorf("unsupported platform %s", target)
		}
		dir := filepath.Join(root, "resources", parts[0]+"-"+parts[1], "bin")
		if err = os.MkdirAll(dir, 0755); err != nil {
			return err
		}
		name := "pansou"
		if parts[0] == "windows" {
			name += ".exe"
		}
		env := append(os.Environ(), "GOTOOLCHAIN=go1.27.4", "CGO_ENABLED=0", "GOOS="+parts[0], "GOARCH="+parts[1], "GOCACHE="+filepath.Join(cache, "go-build"), "GOMODCACHE="+filepath.Join(cache, "go-mod"))
		fmt.Printf("Building bundled PanSou for %s (%d plugins, user-configured channels)…\n", target, len(c.Plugins))
		// Keep security fixes reproducible without rewriting upstream go.mod.
		// New upstream dependencies require an explicit manifest/license review.
		if err = run(source, env, "go", "build", "-modfile="+filepath.Join(root, "buildtools", "pansou.mod"), "-mod=readonly", "-trimpath", "-ldflags=-s -w", "-o", filepath.Join(dir, name), "."); err != nil {
			return err
		}
	}
	return nil
}
