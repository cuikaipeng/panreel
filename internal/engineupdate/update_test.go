package engineupdate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cuikaipeng/panreel/internal/catalog"
)

func hash(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func testArchive(t *testing.T, extra *tar.Header) []byte {
	t.Helper()
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	tw := tar.NewWriter(gz)
	for name, data := range map[string]string{"pansou": "example binary", "licenses/PanSou-LICENSE.txt": "MIT"} {
		if err := tw.WriteHeader(&tar.Header{Name: name, Size: int64(len(data)), Mode: 0700, Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(data)); err != nil {
			t.Fatal(err)
		}
	}
	if extra != nil {
		if err := tw.WriteHeader(extra); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

type fixture struct {
	u                 *Updater
	r                 Release
	key               ed25519.PrivateKey
	manifest, archive []byte
	status            int
}

func setup(t *testing.T) *fixture {
	t.Helper()
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{key: key, archive: testArchive(t, nil), status: 200}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/engine-update.json" {
			w.WriteHeader(f.status)
			_, _ = w.Write(f.manifest)
		} else {
			_, _ = w.Write(f.archive)
		}
	}))
	t.Cleanup(server.Close)
	f.u = &Updater{Dir: t.TempDir(), HTTP: server.Client(), Feed: server.URL + "/engine-update.json", Base: server.URL + "/", Target: "darwin/arm64", Key: pub}
	f.r = Release{Protocol: 1, Revision: strings.Repeat("a", 40), BuiltAt: time.Now().UTC(), Catalog: catalog.Catalog{Revision: strings.Repeat("a", 40), Plugins: []catalog.Plugin{{ID: "example", Env: []string{"EXAMPLE_TOKEN"}}}}, Assets: map[string]Asset{}}
	f.resign(t)
	return f
}
func (f *fixture) resign(t *testing.T) {
	t.Helper()
	f.r.Assets[f.u.Target] = Asset{URL: f.u.Base + "engine.tar.gz", Size: int64(len(f.archive)), SHA256: hash(f.archive), BinarySHA256: hash([]byte("example binary"))}
	data, err := Sign(f.r, f.key)
	if err != nil {
		t.Fatal(err)
	}
	f.manifest = data
	f.r, err = f.u.Verify(data)
	if err != nil {
		t.Fatal(err)
	}
}

func TestVerifiedDownloadActivationAndRestart(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	r, err := f.u.Check(ctx, "older", time.Time{})
	if err != nil || r == nil {
		t.Fatal(err)
	}
	var progress int64
	installed, err := f.u.Prepare(ctx, *r, func(n, total int64) {
		progress = n
		if n > total {
			t.Error("progress exceeds size")
		}
	})
	if err != nil || installed == nil || progress != int64(len(f.archive)) {
		t.Fatal(installed, progress, err)
	}
	if active, err := f.u.Active(); err != nil || active != nil {
		t.Fatal("download activated before probe", err)
	}
	if err = f.u.Activate(installed); err != nil {
		t.Fatal(err)
	}
	reopened := *f.u
	active, err := reopened.Active()
	if err != nil || active == nil || active.ID != installed.ID {
		t.Fatal("activation did not persist", err)
	}
	if r, err = f.u.Check(ctx, active.Release.Revision, time.Time{}); err != nil || r != nil {
		t.Fatal("same revision offered again", err)
	}
	if r, err = f.u.Check(ctx, "other", time.Now().Add(time.Hour)); err != nil || r != nil {
		t.Fatal("older release offered", err)
	}
	if err = os.WriteFile(active.Binary, []byte("tampered"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err = f.u.Active(); err == nil {
		t.Fatal("tampered installed binary accepted")
	}
	if err = f.u.Activate(nil); err != nil {
		t.Fatal(err)
	}
	if active, err = f.u.Active(); err != nil || active != nil {
		t.Fatal("bundled fallback not selected", err)
	}
}

func TestManifestRejectsUntrustedOrIncompatibleUpdates(t *testing.T) {
	for _, mutation := range []string{"signature", "protocol", "url", "catalog", "target", "size", "key"} {
		t.Run(mutation, func(t *testing.T) {
			f := setup(t)
			r := f.r
			switch mutation {
			case "protocol":
				r.Protocol = 2
			case "url":
				a := r.Assets[f.u.Target]
				a.URL = "https://example.org/engine.tar.gz"
				r.Assets[f.u.Target] = a
			case "catalog":
				r.Catalog.Revision = strings.Repeat("b", 40)
			case "target":
				r.Assets = map[string]Asset{}
			case "size":
				a := r.Assets[f.u.Target]
				a.Size = maxArchive + 1
				r.Assets[f.u.Target] = a
			case "key":
				f.u.Key = make([]byte, 32)
			}
			data, err := Sign(r, f.key)
			if err != nil {
				t.Fatal(err)
			}
			if mutation == "signature" {
				var e envelope
				_ = json.Unmarshal(data, &e)
				e.Payload = bytes.Replace(e.Payload, []byte("example"), []byte("altered"), 1)
				data, _ = json.Marshal(e)
			}
			if _, err = f.u.Verify(data); err == nil {
				t.Fatal("invalid update accepted")
			}
		})
	}
}

func TestCorruptionAndCancellationKeepPreviousEngine(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	old, err := f.u.Prepare(ctx, f.r, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.u.Activate(old); err != nil {
		t.Fatal(err)
	}
	f.archive = append(f.archive, 0)
	f.resign(t)
	// A signed expected archive must not accept different downloaded bytes.
	f.archive = append(f.archive, 1)
	if _, err = f.u.Prepare(ctx, f.r, nil); err == nil {
		t.Fatal("corrupt download accepted")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = f.u.Prepare(cancelled, f.r, nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	active, err := f.u.Active()
	if err != nil || active.ID != old.ID {
		t.Fatal("failed download replaced current engine", err)
	}
	staging, _ := filepath.Glob(filepath.Join(f.u.Dir, ".download-*"))
	if len(staging) != 0 {
		t.Fatal("partial downloads left behind")
	}
}

func TestArchiveRejectsTraversalLinksAndDuplicateFiles(t *testing.T) {
	for _, h := range []*tar.Header{{Name: "../escaped", Typeflag: tar.TypeReg}, {Name: "licenses/../../escaped", Typeflag: tar.TypeReg}, {Name: "licenses/link", Typeflag: tar.TypeSymlink, Linkname: "/tmp"}, {Name: "pansou", Typeflag: tar.TypeReg}} {
		t.Run(h.Name, func(t *testing.T) {
			f := setup(t)
			f.archive = testArchive(t, h)
			f.resign(t)
			if _, err := f.u.Prepare(context.Background(), f.r, nil); err == nil {
				t.Fatal("unsafe archive accepted")
			}
		})
	}
}

func TestUnpublishedChannelIsNotReportedAsCurrent(t *testing.T) {
	f := setup(t)
	f.status = 404
	if _, err := f.u.Check(context.Background(), "", time.Time{}); !errors.Is(err, ErrNotPublished) {
		t.Fatal(err)
	}
}
