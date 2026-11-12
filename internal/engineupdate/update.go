// Package engineupdate distributes desktop-adapted PanSou independently of the UI.
package engineupdate

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/cuikaipeng/panreel/internal/catalog"
	"github.com/cuikaipeng/panreel/internal/model"
	"github.com/cuikaipeng/panreel/internal/network"
)

const (
	Protocol        = 1
	CheckInterval   = 30 * time.Minute
	AssetBase       = "https://github.com/cuikaipeng/panreel/releases/download/pansou-engine/"
	FeedURL         = AssetBase + "engine-update.json"
	PublicKey       = "AH62F0dxidF1AEa4gvGGovK+In8DZoHWCbPbZWkKn4I="
	maxArchive      = 128 << 20
	signatureDomain = "PanReel engine update v1\n"
)

var ErrNotPublished = errors.New("engine update channel has not been published")
var digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
var revisionPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
var pluginPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,100}$`)
var envPattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,127}$`)

type Asset struct {
	URL          string `json:"url"`
	Size         int64  `json:"size"`
	SHA256       string `json:"sha256"`
	BinarySHA256 string `json:"binary_sha256"`
}
type Release struct {
	Protocol int              `json:"protocol"`
	Revision string           `json:"revision"`
	BuiltAt  time.Time        `json:"built_at"`
	Catalog  catalog.Catalog  `json:"catalog"`
	Assets   map[string]Asset `json:"assets"`
	Signed   []byte           `json:"-"`
}
type envelope struct {
	Payload   json.RawMessage `json:"payload"`
	Signature string          `json:"signature"`
}
type Installed struct {
	Release    Release
	ID, Binary string
}
type Updater struct {
	Dir                string
	HTTP               *http.Client
	Feed, Base, Target string
	Key                ed25519.PublicKey
}

func New(dir string, settings model.Settings) *Updater {
	key, _ := base64.StdEncoding.DecodeString(PublicKey)
	transport := network.Transport(settings)
	return &Updater{Dir: filepath.Join(dir, "engines"), HTTP: &http.Client{Transport: transport, Timeout: 10 * time.Minute,
		CheckRedirect: func(r *http.Request, via []*http.Request) error {
			if r.URL.Scheme != "https" || len(via) > 8 {
				return errors.New("invalid engine download redirect")
			}
			return nil
		}}, Feed: FeedURL, Base: AssetBase, Target: runtime.GOOS + "/" + runtime.GOARCH, Key: key}
}

func Sign(r Release, key ed25519.PrivateKey) ([]byte, error) {
	payload, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	signature := ed25519.Sign(key, append([]byte(signatureDomain), payload...))
	return json.Marshal(envelope{Payload: payload, Signature: base64.StdEncoding.EncodeToString(signature)})
}

func (u *Updater) Verify(data []byte) (Release, error) {
	var e envelope
	var r Release
	if len(data) > 1<<20 || json.Unmarshal(data, &e) != nil {
		return r, errors.New("invalid engine manifest")
	}
	sig, err := base64.StdEncoding.DecodeString(e.Signature)
	if err != nil || len(u.Key) != ed25519.PublicKeySize || !ed25519.Verify(u.Key, append([]byte(signatureDomain), e.Payload...), sig) {
		return r, errors.New("engine manifest signature failed")
	}
	if err = json.Unmarshal(e.Payload, &r); err != nil {
		return r, err
	}
	if r.Protocol != Protocol || !revisionPattern.MatchString(r.Revision) || r.Catalog.Revision != r.Revision || r.BuiltAt.IsZero() || r.BuiltAt.After(time.Now().Add(24*time.Hour)) {
		return r, errors.New("incompatible engine manifest")
	}
	if len(r.Catalog.Plugins) == 0 || len(r.Catalog.Plugins) > 1000 {
		return r, errors.New("invalid engine plugin catalog")
	}
	seen := map[string]bool{}
	for _, p := range r.Catalog.Plugins {
		if !pluginPattern.MatchString(p.ID) || seen[p.ID] {
			return r, errors.New("invalid engine plugin ID")
		}
		seen[p.ID] = true
		for _, key := range p.Env {
			if !envPattern.MatchString(key) {
				return r, errors.New("invalid engine configuration key")
			}
		}
	}
	a, ok := r.Assets[u.Target]
	if !ok || !digestPattern.MatchString(a.SHA256) || !digestPattern.MatchString(a.BinarySHA256) || a.Size <= 0 || a.Size > maxArchive {
		return r, errors.New("no compatible engine download")
	}
	name := strings.TrimPrefix(a.URL, u.Base)
	parsed, err := url.Parse(a.URL)
	if err != nil || !strings.HasPrefix(a.URL, u.Base) || name == "" || strings.ContainsAny(name, "/\\%?#") || !strings.HasSuffix(name, ".tar.gz") || parsed.User != nil {
		return r, errors.New("untrusted engine download URL")
	}
	r.Signed = bytes.Clone(data)
	return r, nil
}

func (u *Updater) get(ctx context.Context, address string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "PanReel-engine-updater/1")
	response, err := u.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		if response.StatusCode == 404 && address == u.Feed {
			return nil, ErrNotPublished
		}
		return nil, fmt.Errorf("engine update server returned %d", response.StatusCode)
	}
	return response, nil
}

func (u *Updater) Check(ctx context.Context, current string, installedAt time.Time) (*Release, error) {
	response, err := u.get(ctx, u.Feed)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil {
		return nil, err
	}
	r, err := u.Verify(data)
	if err != nil {
		return nil, err
	}
	if r.Revision == current || !r.BuiltAt.After(installedAt) {
		return nil, nil
	}
	return &r, nil
}

func executable(target string) string {
	if strings.HasPrefix(target, "windows/") {
		return "pansou.exe"
	}
	return "pansou"
}

func (u *Updater) Read(id string) (*Installed, error) {
	if !digestPattern.MatchString(id) {
		return nil, errors.New("invalid installed engine ID")
	}
	dir := filepath.Join(u.Dir, id)
	data, err := os.ReadFile(filepath.Join(dir, "release.json"))
	if err != nil {
		return nil, err
	}
	r, err := u.Verify(data)
	if err != nil {
		return nil, err
	}
	asset := r.Assets[u.Target]
	if asset.SHA256 != id {
		return nil, errors.New("installed engine ID mismatch")
	}
	binary := filepath.Join(dir, executable(u.Target))
	info, err := os.Lstat(binary)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("invalid installed binary")
	}
	f, err := os.Open(binary)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(f, (256<<20)+1))
	if err != nil {
		return nil, err
	}
	if n > 256<<20 || hex.EncodeToString(h.Sum(nil)) != asset.BinarySHA256 {
		return nil, errors.New("installed engine checksum failed")
	}
	return &Installed{Release: r, ID: id, Binary: binary}, nil
}
func (u *Updater) Active() (*Installed, error) {
	data, err := os.ReadFile(filepath.Join(u.Dir, "active"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return u.Read(strings.TrimSpace(string(data)))
}
func (u *Updater) Activate(installed *Installed) error {
	if installed == nil {
		err := os.Remove(filepath.Join(u.Dir, "active"))
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if _, err := u.Read(installed.ID); err != nil {
		return err
	}
	f, err := os.CreateTemp(u.Dir, ".active-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = io.WriteString(f, installed.ID+"\n")
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), filepath.Join(u.Dir, "active"))
}

func (u *Updater) Prepare(ctx context.Context, release Release, progress func(int64, int64)) (*Installed, error) {
	r, err := u.Verify(release.Signed)
	if err != nil {
		return nil, err
	}
	a := r.Assets[u.Target]
	if ready, e := u.Read(a.SHA256); e == nil {
		return ready, nil
	}
	if err = os.MkdirAll(u.Dir, 0700); err != nil {
		return nil, err
	}
	stage, err := os.MkdirTemp(u.Dir, ".download-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(stage)
	response, err := u.get(ctx, a.URL)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	f, err := os.OpenFile(filepath.Join(stage, "archive"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	h := sha256.New()
	reader := &progressReader{reader: io.LimitReader(response.Body, a.Size+1), total: a.Size, notify: progress}
	n, err := io.Copy(io.MultiWriter(f, h), reader)
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return nil, err
	}
	if n != a.Size || hex.EncodeToString(h.Sum(nil)) != a.SHA256 {
		return nil, errors.New("engine archive checksum failed")
	}
	if err = extract(ctx, filepath.Join(stage, "archive"), stage, executable(u.Target)); err != nil {
		return nil, err
	}
	if err = os.Remove(filepath.Join(stage, "archive")); err != nil {
		return nil, err
	}
	if err = os.WriteFile(filepath.Join(stage, "release.json"), r.Signed, 0600); err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	dest := filepath.Join(u.Dir, a.SHA256)
	// A failed prior download never becomes active. Replace only its inactive
	// corrupt directory; never overwrite an active executable (Windows locks it).
	if _, err = os.Stat(dest); err == nil {
		active, _ := os.ReadFile(filepath.Join(u.Dir, "active"))
		if strings.TrimSpace(string(active)) == a.SHA256 {
			return nil, errors.New("active engine is damaged; restore the bundled engine first")
		}
		if err = os.RemoveAll(dest); err != nil {
			return nil, err
		}
	}
	if err = os.Rename(stage, dest); err != nil {
		return nil, err
	}
	return u.Read(a.SHA256)
}

type progressReader struct {
	reader            io.Reader
	downloaded, total int64
	notify            func(int64, int64)
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, e := p.reader.Read(b)
	p.downloaded += int64(n)
	if p.notify != nil {
		p.notify(p.downloaded, p.total)
	}
	return n, e
}
