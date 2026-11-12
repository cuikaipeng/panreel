package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckoutRevisionInitializesEmptyCacheAndReplacesPatchedRevision(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("Git is required to prepare PanSou")
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	git := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	files := []string{"main.go", "config/config.go", "plugin/plugin.go", "service/search_service.go"}
	write := func(dir, content string) {
		t.Helper()
		for _, name := range files {
			path := filepath.Join(dir, name)
			if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(content), 0644); err != nil {
				t.Fatal(err)
			}
		}
	}
	origin, cache := t.TempDir(), t.TempDir()
	git(origin, "init")
	commit := func(content string) string {
		write(origin, content)
		git(origin, "add", ".")
		git(origin, "-c", "user.name=PanReel tests", "-c", "user.email=tests@example.invalid", "-c", "commit.gpgSign=false", "commit", "-m", content)
		return git(origin, "rev-parse", "HEAD")
	}
	first := commit("first revision\n")
	git(cache, "init")
	git(cache, "remote", "add", "origin", origin)
	if err := checkoutRevision(cache, first); err != nil {
		t.Fatalf("fresh cache failed: %v", err)
	}
	if got := git(cache, "rev-parse", "HEAD"); got != first {
		t.Fatalf("fresh cache HEAD = %s, want %s", got, first)
	}
	write(cache, "generated desktop patch\n")
	if err := checkoutRevision(cache, first); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(cache, "main.go")); err != nil || string(data) != "generated desktop patch\n" {
		t.Fatal("same revision unexpectedly changed cached files", err)
	}
	second := commit("second revision\n")
	if err := checkoutRevision(cache, second); err != nil {
		t.Fatalf("updating patched cache failed: %v", err)
	}
	for _, name := range files {
		if data, err := os.ReadFile(filepath.Join(cache, name)); err != nil || string(data) != "second revision\n" {
			t.Fatalf("%s did not update to the second revision: %v", name, err)
		}
	}
}
