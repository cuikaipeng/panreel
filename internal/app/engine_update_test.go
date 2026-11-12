package app

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/cuikaipeng/panreel/internal/catalog"
	"github.com/cuikaipeng/panreel/internal/engine"
	"github.com/cuikaipeng/panreel/internal/engineupdate"
	"github.com/cuikaipeng/panreel/internal/model"
	"github.com/cuikaipeng/panreel/internal/storage"
)

func TestRealEngineUpdateAndRollback(t *testing.T) {
	if os.Getenv("PANREEL_ENGINE_TEST") != "1" {
		t.Skip("requires prepared desktop engine")
	}
	defer catalog.SetCurrent(catalog.Bundled)
	a := newApp()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	a.ctx = ctx
	dir := t.TempDir()
	resources, err := filepath.Abs("../../resources")
	if err != nil {
		t.Fatal(err)
	}
	a.store, err = storage.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer a.store.Close()
	a.settings.Channels = []string{"kept_channel"}
	a.settings.CustomChannels = []string{"kept_channel", "disabled_channel"}
	a.restoreDraft()
	a.engine = &engine.Manager{Resources: resources, DataDir: dir}
	defer a.engine.Stop()
	if err = a.engine.Start(ctx, a.settings); err != nil {
		t.Fatal(err)
	}
	if err = a.store.SaveSettings(a.settings); err != nil {
		t.Fatal(err)
	}
	if err = a.store.Bookmark(model.Item{ID: "preserved", Title: "Preserved favorite"}, true); err != nil {
		t.Fatal(err)
	}
	binary, err := os.ReadFile(a.engine.Binary())
	if err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	tw := tar.NewWriter(gz)
	name := "pansou"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	for filename, data := range map[string][]byte{name: binary, "licenses/PanSou-LICENSE.txt": []byte("MIT test fixture")} {
		if err = tw.WriteHeader(&tar.Header{Name: filename, Size: int64(len(data)), Mode: 0700, Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err = tw.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err = tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err = gz.Close(); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(archive.Bytes()) }))
	defer server.Close()
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	u := engineupdate.New(dir, model.Defaults())
	u.HTTP = server.Client()
	u.Key = pub
	u.Base = server.URL + "/"
	sum := func(data []byte) string { h := sha256.Sum256(data); return hex.EncodeToString(h[:]) }
	r := engineupdate.Release{Protocol: 1, Revision: catalog.Bundled.Revision, Catalog: catalog.Bundled, BuiltAt: time.Now(), Assets: map[string]engineupdate.Asset{u.Target: {URL: u.Base + "engine.tar.gz", Size: int64(archive.Len()), SHA256: sum(archive.Bytes()), BinarySHA256: sum(binary)}}}
	signed, err := engineupdate.Sign(r, key)
	if err != nil {
		t.Fatal(err)
	}
	r, err = u.Verify(signed)
	if err != nil {
		t.Fatal(err)
	}
	oldFactory := newEngineUpdater
	defer func() { newEngineUpdater = oldFactory }()
	newEngineUpdater = func(string, model.Settings) *engineupdate.Updater { return u }
	queue := make(chan func(), 8)
	a.dispatch = func(fn func()) { queue <- fn }
	a.engineAvailable = &r
	a.online = true
	a.installEngineUpdate()
	for a.engineUpdating {
		select {
		case fn := <-queue:
			fn()
		case <-ctx.Done():
			t.Fatal("engine update timed out")
		}
	}
	a.engineWork.Wait()
	if a.engineInstalled == nil || a.engineUpdateStatus.key != "PanSou updated. No app restart required." {
		t.Fatal(a.engineUpdateStatus)
	}
	active, err := u.Active()
	if err != nil || active == nil {
		t.Fatal("updated engine did not persist", err)
	}
	health, err := a.engine.Client().Health(ctx)
	if err != nil || len(health.Channels) != 1 || health.Channels[0] != "kept_channel" {
		t.Fatal("channels changed", err)
	}
	loaded, err := a.store.Settings()
	if err != nil || len(loaded.CustomChannels) != 2 {
		t.Fatal("disabled channel lost", err)
	}
	saved, err := a.store.Bookmarks()
	if err != nil || len(saved) != 1 {
		t.Fatal("favorites changed", err)
	}
	// A replacement that cannot start must preserve the installed engine,
	// active pointer and settings, and resume serving requests immediately.
	broken := *active
	broken.Binary = filepath.Join(dir, "does-not-exist")
	err = a.switchEngine(ctx, u, &broken, a.settings, active, a.settings, catalog.Current())
	if err == nil {
		t.Fatal("broken engine accepted")
	}
	if _, err = a.engine.Client().Health(ctx); err != nil {
		t.Fatal("rollback left engine offline", err)
	}
	after, err := u.Active()
	if err != nil || after.ID != active.ID {
		t.Fatal("rollback lost active version", err)
	}
	// Simulate a valid installed catalog with a newly introduced plugin.
	// A user who disabled only that plugin must keep it disabled on restart.
	updated := active.Release
	updated.Catalog.Plugins = append(append([]catalog.Plugin{}, updated.Catalog.Plugins...), catalog.Plugin{ID: "new_plugin"})
	signed, err = engineupdate.Sign(updated, key)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(filepath.Dir(active.Binary), "release.json"), signed, 0600); err != nil {
		t.Fatal(err)
	}
	reopened := newApp()
	reopened.settings = a.settings.Clone()
	reopened.engine = &engine.Manager{Resources: resources, DataDir: dir}
	reopened.loadEngineVersion()
	if len(reopened.settings.Plugins) != len(catalog.Bundled.Plugins) || len(catalog.Current().Plugins) != len(catalog.Bundled.Plugins)+1 {
		t.Fatal("restart re-enabled a disabled new plugin")
	}
	// A crash can leave newer settings with the old/bundled pointer.
	if err = u.Activate(nil); err != nil {
		t.Fatal(err)
	}
	reopened = newApp()
	reopened.engine = &engine.Manager{Resources: resources, DataDir: dir}
	reopened.loadEngineVersion()
	if err = reopened.settings.Validate(); err != nil {
		t.Fatal("interrupted activation prevented fallback startup", err)
	}
}
