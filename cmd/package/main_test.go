package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestReleaseRejectsMismatchedKeysAndModifiedArchives(t *testing.T) {
	pub, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config{Version: "1.2.3"}
	cfg.Updates.PublicKey = base64.StdEncoding.EncodeToString(pub)
	cfg.Updates.GitHub = "cuikaipeng/panreel"
	if err = validateKey(cfg, base64.StdEncoding.EncodeToString(private)); err != nil {
		t.Fatal(err)
	}
	_, wrong, _ := ed25519.GenerateKey(rand.Reader)
	if validateKey(cfg, base64.StdEncoding.EncodeToString(wrong)) == nil {
		t.Fatal("accepted a different app's signing key")
	}
	dir := t.TempDir()
	archive := "panreel-1.2.3-darwin-universal.tar.gz"
	data := []byte("release archive fixture")
	sum := sha256.Sum256(data)
	m := manifest{Version: cfg.Version, URL: "https://github.com/cuikaipeng/panreel/releases/download/v1.2.3/" + archive, Size: int64(len(data)), Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(private, sum[:]))}
	metadata, _ := json.Marshal(m)
	if err = os.WriteFile(filepath.Join(dir, archive), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "update-darwin-universal.json"), metadata, 0600); err != nil {
		t.Fatal(err)
	}
	if err = verifyRelease(cfg, dir); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, archive), []byte("corrupt archive fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	if verifyRelease(cfg, dir) == nil {
		t.Fatal("accepted a corrupt archive")
	}
	if err = os.WriteFile(filepath.Join(dir, archive), data, 0600); err != nil {
		t.Fatal(err)
	}
	m.URL = "https://example.com/" + archive
	metadata, _ = json.Marshal(m)
	_ = os.WriteFile(filepath.Join(dir, "update-darwin-universal.json"), metadata, 0600)
	if verifyRelease(cfg, dir) == nil {
		t.Fatal("accepted an unexpected release origin")
	}
}
