// licenses collects notices for the exact modules used in both shipped apps.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type module struct {
	Path, Version, Dir string
	Main               bool
}
type pkg struct{ Module *module }

func main() {
	check := flag.Bool("check", false, "verify committed notices without changing them; fail on license or dependency drift")
	flag.Parse()
	if err := collect(*check); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func collect(check bool) error {
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	engineDir := filepath.Join(root, ".cache", "pansou", "source")
	files := map[string][]byte{}
	for source, name := range map[string]string{
		filepath.Join(root, "LICENSE"):                "PanReel-LICENSE.txt",
		filepath.Join(root, "THIRD_PARTY_NOTICES.md"): "NOTICE.txt",
		filepath.Join(engineDir, "LICENSE"):           "PanSou-LICENSE.txt",
	} {
		data, err := os.ReadFile(source)
		if err != nil {
			return fmt.Errorf("read license source (run cmd/prepare first): %w", err)
		}
		files[name] = data
	}
	engineNotices, err := moduleNotices(engineDir)
	if err != nil {
		return fmt.Errorf("PanSou: %w", err)
	}
	for name, data := range engineNotices {
		if name != "LICENSE" {
			files[filepath.Join("dependencies", "pansou", name)] = data
		}
	}
	modules := map[string]module{}
	for _, engine := range []bool{false, true} {
		for _, target := range []struct{ os, arch string }{{"darwin", "arm64"}, {"darwin", "amd64"}, {"windows", "amd64"}} {
			dir, entry := root, "./cmd/panreel"
			args := []string{"list", "-deps", "-json"}
			env := append(os.Environ(), "CGO_ENABLED=0", "GOOS="+target.os, "GOARCH="+target.arch)
			if engine {
				dir, entry = engineDir, "."
				env = append(env, "GOTOOLCHAIN=go1.27.4", "GOMODCACHE="+filepath.Join(root, ".cache", "pansou", "go-mod"))
				args = append(args, "-modfile="+filepath.Join(root, "buildtools", "pansou.mod"), "-mod=readonly")
			}
			cmd := exec.Command("go", append(args, entry)...)
			cmd.Dir, cmd.Env = dir, env
			data, err := cmd.Output()
			if err != nil {
				return fmt.Errorf("dependency inventory (%s/%s, engine %v): %w", target.os, target.arch, engine, err)
			}
			dec := json.NewDecoder(bytes.NewReader(data))
			for {
				var p pkg
				if err = dec.Decode(&p); err == io.EOF {
					break
				}
				if err != nil {
					return err
				}
				if p.Module != nil && !p.Module.Main {
					modules[p.Module.Path+"@"+p.Module.Version] = *p.Module
				}
			}
		}
	}
	goRoot, err := exec.Command("go", "env", "GOROOT").Output()
	if err != nil {
		return err
	}
	rootPath := strings.TrimSpace(string(goRoot))
	goLicense, err := os.ReadFile(filepath.Join(rootPath, "LICENSE"))
	if os.IsNotExist(err) {
		goLicense, err = os.ReadFile(filepath.Join(filepath.Dir(rootPath), "LICENSE"))
	}
	if err != nil {
		return err
	}
	files["Go-LICENSE.txt"] = goLicense
	keys := make([]string, 0, len(modules))
	for key := range modules {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var index strings.Builder
	index.WriteString("PanReel and bundled PanSou — dependency notices\n\n")
	for _, key := range keys {
		m := modules[key]
		notices, err := moduleNotices(m.Dir)
		if err != nil {
			return fmt.Errorf("%s: %w", key, err)
		}
		name := strings.NewReplacer("/", "_", "\\", "_", ":", "_").Replace(key)
		for rel, data := range notices {
			files[filepath.Join("dependencies", name, rel)] = data
		}
		fmt.Fprintf(&index, "%s (%d notice files)\n", key, len(notices))
	}
	files["DEPENDENCIES.txt"] = []byte(index.String())
	for name, data := range files {
		files[name] = bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
	}
	if err = syncNotices(filepath.Join(root, "resources", "licenses"), files, check); err != nil {
		return err
	}
	verb := "Collected"
	if check {
		verb = "Verified"
	}
	fmt.Printf("%s licenses for %d Go module versions across all three desktop targets.\n", verb, len(keys))
	return nil
}

func isNotice(name string) bool {
	name = strings.ToLower(name)
	for _, prefix := range []string{"license", "licence", "notice", "copying", "copyright", "authors", "ofl"} {
		if name == prefix || strings.HasPrefix(name, prefix+".") || strings.HasPrefix(name, prefix+"-") {
			return true
		}
	}
	return false
}

func moduleNotices(dir string) (map[string][]byte, error) {
	files := map[string][]byte{}
	licenseFound := false
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == "testdata" || d.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if !isNotice(d.Name()) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() > 1024*1024 {
			return fmt.Errorf("license is not a regular file of at most 1 MiB: %s", path)
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[rel] = data
		name := strings.ToLower(d.Name())
		if strings.HasPrefix(name, "license") || strings.HasPrefix(name, "licence") || strings.HasPrefix(name, "copying") || strings.HasPrefix(name, "ofl") {
			licenseFound = true
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if !licenseFound {
		return nil, fmt.Errorf("no license found")
	}
	return files, nil
}

// Only generated dependency files are removed. Manually maintained attribution
// files (Kivrix, kero, icons) remain untouched. Check mode never writes anything.
func syncNotices(dest string, files map[string][]byte, check bool) error {
	var changed, stale []string
	for name, expected := range files {
		actual, err := os.ReadFile(filepath.Join(dest, name))
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if err != nil || !bytes.Equal(actual, expected) {
			changed = append(changed, name)
		}
	}
	depDir := filepath.Join(dest, "dependencies")
	err := filepath.WalkDir(depDir, func(path string, d fs.DirEntry, err error) error {
		if os.IsNotExist(err) && path == depDir {
			return nil
		}
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dest, path)
		if err != nil {
			return err
		}
		if _, ok := files[rel]; !ok {
			stale = append(stale, rel)
		}
		return nil
	})
	if err != nil {
		return err
	}
	sort.Strings(changed)
	sort.Strings(stale)
	if check && (len(changed) != 0 || len(stale) != 0) {
		return fmt.Errorf("license review required: %d missing/changed and %d obsolete notices; review upstream terms, run go run ./cmd/licenses, and commit the reviewed notices\nchanged: %s\nobsolete: %s", len(changed), len(stale), strings.Join(changed, ", "), strings.Join(stale, ", "))
	}
	for _, name := range changed {
		path := filepath.Join(dest, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return err
		}
		if err := os.WriteFile(path, files[name], 0644); err != nil {
			return err
		}
	}
	for _, name := range stale {
		path := filepath.Join(dest, name)
		if err := os.Remove(path); err != nil {
			return err
		}
		for dir := filepath.Dir(path); dir != depDir; dir = filepath.Dir(dir) {
			if err := os.Remove(dir); err != nil {
				break
			}
		}
	}
	return nil
}
