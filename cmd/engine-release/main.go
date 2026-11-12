// engine-release assembles or signs the independent PanSou update channel.
package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/cuikaipeng/panreel/internal/catalog"
	"github.com/cuikaipeng/panreel/internal/engineupdate"
	"github.com/cuikaipeng/panreel/internal/model"
)

func main() {
	out := flag.String("out", "build/engine-release", "output directory")
	sign := flag.Bool("sign", false, "verify assembled assets and sign the manifest; never executes an engine")
	flag.Parse()
	var err error
	if *sign {
		err = signRelease(*out)
	} else {
		err = assemble(*out)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func digest(name string) (string, int64, error) {
	f, err := os.Open(name)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil)), n, err
}

func archive(target, dest string) error {
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	add := func(local, name string) error {
		info, e := os.Stat(local)
		if e != nil {
			return e
		}
		if !info.Mode().IsRegular() {
			return errors.New("invalid release file")
		}
		if e = tw.WriteHeader(&tar.Header{Name: name, Size: info.Size(), Mode: 0600, Typeflag: tar.TypeReg}); e != nil {
			return e
		}
		in, e := os.Open(local)
		if e != nil {
			return e
		}
		defer in.Close()
		_, e = io.Copy(tw, in)
		return e
	}
	binary := "pansou"
	if strings.HasPrefix(target, "windows") {
		binary += ".exe"
	}
	err = add(filepath.Join("resources", strings.ReplaceAll(target, "/", "-"), "bin", binary), binary)
	if err == nil {
		err = filepath.WalkDir("resources/licenses", func(p string, d fs.DirEntry, e error) error {
			if e != nil {
				return e
			}
			if d.IsDir() {
				return nil
			}
			rel, e := filepath.Rel("resources", p)
			if e != nil {
				return e
			}
			return add(p, filepath.ToSlash(rel))
		})
	}
	err = errors.Join(err, tw.Close(), gz.Close(), f.Close())
	return err
}

func assemble(out string) error {
	if err := os.MkdirAll(out, 0700); err != nil {
		return err
	}
	r := engineupdate.Release{Protocol: engineupdate.Protocol, Revision: catalog.Bundled.Revision, BuiltAt: time.Now().UTC().Truncate(time.Second), Catalog: catalog.Bundled, Assets: map[string]engineupdate.Asset{}}
	for _, target := range []string{"darwin/arm64", "darwin/amd64", "windows/amd64"} {
		temp := filepath.Join(out, strings.ReplaceAll(target, "/", "-")+".tar.gz")
		if err := archive(target, temp); err != nil {
			return err
		}
		hash, size, err := digest(temp)
		if err != nil {
			return err
		}
		binary := "pansou"
		if strings.HasPrefix(target, "windows") {
			binary += ".exe"
		}
		binaryHash, _, err := digest(filepath.Join("resources", strings.ReplaceAll(target, "/", "-"), "bin", binary))
		if err != nil {
			return err
		}
		name := "pansou-" + r.Revision[:12] + "-" + strings.ReplaceAll(target, "/", "-") + "-" + hash[:12] + ".tar.gz"
		if err = os.Rename(temp, filepath.Join(out, name)); err != nil {
			return err
		}
		r.Assets[target] = engineupdate.Asset{URL: engineupdate.AssetBase + name, Size: size, SHA256: hash, BinarySHA256: binaryHash}
	}
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(out, "engine-payload.json"), data, 0600)
}

func signRelease(out string) error {
	data, err := os.ReadFile(filepath.Join(out, "engine-payload.json"))
	if err != nil {
		return err
	}
	var r engineupdate.Release
	if err = json.Unmarshal(data, &r); err != nil {
		return err
	}
	private, err := base64.StdEncoding.DecodeString(strings.TrimSpace(os.Getenv("MYGO_UPDATER_PRIVATE_KEY")))
	if err != nil || len(private) != ed25519.PrivateKeySize {
		return errors.New("MYGO_UPDATER_PRIVATE_KEY is required")
	}
	public, _ := base64.StdEncoding.DecodeString(engineupdate.PublicKey)
	if !slices.Equal(ed25519.NewKeyFromSeed(private[:32]), private) || !slices.Equal(private[32:], public) {
		return errors.New("engine signing key does not match PanReel")
	}
	data, err = engineupdate.Sign(r, private)
	if err != nil {
		return err
	}
	if len(r.Assets) != 3 {
		return errors.New("expected all three desktop engine targets")
	}
	for target, asset := range r.Assets {
		u := engineupdate.New("", model.Defaults())
		u.Target = target
		if _, err = u.Verify(data); err != nil {
			return err
		}
		name := strings.TrimPrefix(asset.URL, engineupdate.AssetBase)
		hash, size, e := digest(filepath.Join(out, name))
		if e != nil {
			return e
		}
		if hash != asset.SHA256 || size != asset.Size {
			return errors.New("release asset checksum mismatch")
		}
	}
	return os.WriteFile(filepath.Join(out, "engine-update.json"), data, 0644)
}
