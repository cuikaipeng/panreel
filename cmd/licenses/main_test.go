package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFixture(t *testing.T, root, name, text string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestModuleNoticesIncludesComponentNoticesAndAttribution(t *testing.T) {
	root := t.TempDir()
	want := map[string]string{
		"LICENSE":   "root license",
		"NOTICE.md": "required attribution",
		"AUTHORS":   "component authors",
		filepath.Join("component", "COPYING.txt"): "component license",
	}
	for name, text := range want {
		writeFixture(t, root, name, text)
	}
	writeFixture(t, root, "testdata/LICENSE", "test fixture only")
	writeFixture(t, root, "node_modules/example/LICENSE", "not shipped")
	got, err := moduleNotices(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Fatalf("notices: %v", got)
	}
	for name, text := range want {
		if string(got[name]) != text {
			t.Fatalf("lost %s", name)
		}
	}
	unlicensed := t.TempDir()
	writeFixture(t, unlicensed, "AUTHORS", "authors without a license grant")
	if _, err := moduleNotices(unlicensed); err == nil {
		t.Fatal("accepted authors as a license")
	}
}

func TestNoticeCheckDoesNotApproveDriftOrDeleteManualNotices(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "PanSou-LICENSE.txt", "previously reviewed license")
	writeFixture(t, root, "dependencies/removed@v1/LICENSE", "obsolete dependency")
	writeFixture(t, root, "kivrix.txt", "manual attribution")
	want := map[string][]byte{
		"PanSou-LICENSE.txt": []byte("changed upstream license"),
		filepath.Join("dependencies", "current@v2", "NOTICE.md"): []byte("new component notice"),
	}
	if err := syncNotices(root, want, true); err == nil || !strings.Contains(err.Error(), "license review required") {
		t.Fatal("license drift was not blocked", err)
	}
	read, err := os.ReadFile(filepath.Join(root, "PanSou-LICENSE.txt"))
	if err != nil || string(read) != "previously reviewed license" {
		t.Fatal("check changed reviewed license", err)
	}
	if _, err := os.Stat(filepath.Join(root, "dependencies/removed@v1/LICENSE")); err != nil {
		t.Fatal("check removed a file", err)
	}
	if _, err := os.Stat(filepath.Join(root, "dependencies/current@v2/NOTICE.md")); !os.IsNotExist(err) {
		t.Fatal("check wrote a new file", err)
	}
	if err := syncNotices(root, want, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "dependencies/removed@v1/LICENSE")); !os.IsNotExist(err) {
		t.Fatal("obsolete notice remains", err)
	}
	read, err = os.ReadFile(filepath.Join(root, "kivrix.txt"))
	if err != nil || string(read) != "manual attribution" {
		t.Fatal("removed manual attribution", err)
	}
	if err := syncNotices(root, want, true); err != nil {
		t.Fatal("reviewed notice tree does not verify", err)
	}
}
