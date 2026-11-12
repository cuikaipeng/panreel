// package prepares the complete desktop distribution using the pinned MyGo tool.
package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"

	"github.com/cuikaipeng/panreel/internal/engineupdate"
)

type config struct {
	Name, Slug, Version, Main, Out string
	Updates                        struct{ PublicKey, GitHub string }
}
type manifest struct {
	Version, URL, Signature string
	Size                    int64
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	platform := flag.String("platform", runtime.GOOS+"/"+runtime.GOARCH, "comma-separated target platforms")
	check := flag.Bool("check", false, "check release versions without building")
	verify := flag.String("verify", "", "verify signed manifests and write checksums in this release directory")
	requireKey := flag.Bool("require-key", false, "require the release signing key")
	tag := flag.String("tag", "", "require this release tag, e.g. v0.1.0")
	flag.Parse()
	cfg, err := loadConfig(*tag)
	if err != nil {
		return err
	}
	if *check {
		fmt.Println("Version and update configuration verified:", cfg.Version)
		return nil
	}
	if *verify != "" {
		return verifyRelease(cfg, *verify)
	}
	targets := strings.Split(*platform, ",")
	engines := []string{}
	for _, target := range targets {
		switch target {
		case "darwin/universal":
			engines = append(engines, "darwin/arm64", "darwin/amd64")
		case "darwin/arm64", "darwin/amd64", "windows/amd64":
			engines = append(engines, target)
		default:
			return fmt.Errorf("unsupported desktop target %q", target)
		}
	}
	key := strings.TrimSpace(os.Getenv("MYGO_UPDATER_PRIVATE_KEY"))
	if key == "" {
		data, e := os.ReadFile(".local/update-keys/mygo-update.key")
		if e == nil {
			key = strings.TrimSpace(string(data))
		} else if !os.IsNotExist(e) {
			return e
		}
	}
	if key == "" && *requireKey {
		return errors.New("MYGO_UPDATER_PRIVATE_KEY is required for a release")
	}
	if key != "" {
		if err = validateKey(cfg, key); err != nil {
			return err
		}
	}
	if err = command(nil, "go", "run", "./cmd/prepare", "-platform", strings.Join(engines, ",")); err != nil {
		return err
	}
	if err = command(nil, "go", "run", "./cmd/licenses", "-check"); err != nil {
		return err
	}
	args := []string{"tool", "mygo", "build", "-platform", *platform}
	if identity := os.Getenv("PANREEL_MACOS_SIGN_IDENTITY"); identity != "" {
		args = append(args, "-sign", identity)
	}
	if err = command([]string{"MYGO_UPDATER_PRIVATE_KEY=" + key}, "go", args...); err != nil {
		return err
	}
	return nil
}
func command(extra []string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Env = append(os.Environ(), extra...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
func loadConfig(tag string) (config, error) {
	var cfg config
	data, err := os.ReadFile("mygo.json")
	if err != nil {
		return cfg, err
	}
	if err = json.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}
	if !regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`).MatchString(cfg.Version) {
		return cfg, errors.New("version must be stable X.Y.Z")
	}
	app, err := os.ReadFile("internal/app/app.go")
	if err != nil {
		return cfg, err
	}
	match := regexp.MustCompile(`const appVersion = "([^"]+)"`).FindSubmatch(app)
	if len(match) != 2 || string(match[1]) != cfg.Version {
		return cfg, errors.New("appVersion and mygo.json version differ")
	}
	var pkg struct{ Version string }
	data, err = os.ReadFile("package.json")
	if err != nil {
		return cfg, err
	}
	if err = json.Unmarshal(data, &pkg); err != nil {
		return cfg, err
	}
	if pkg.Version != cfg.Version {
		return cfg, errors.New("package.json and mygo.json version differ")
	}
	if tag != "" && tag != "v"+cfg.Version {
		return cfg, fmt.Errorf("expected tag v%s", cfg.Version)
	}
	if cfg.Updates.GitHub != "cuikaipeng/panreel" {
		return cfg, errors.New("unexpected update repository")
	}
	if cfg.Updates.PublicKey != engineupdate.PublicKey {
		return cfg, errors.New("application and engine update public keys differ")
	}
	pub, err := base64.StdEncoding.DecodeString(cfg.Updates.PublicKey)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return cfg, errors.New("invalid update public key")
	}
	return cfg, nil
}
func validateKey(cfg config, key string) error {
	private, err := base64.StdEncoding.DecodeString(key)
	if err != nil || len(private) != ed25519.PrivateKeySize {
		return errors.New("invalid update signing key")
	}
	pub, err := base64.StdEncoding.DecodeString(cfg.Updates.PublicKey)
	if err != nil {
		return err
	}
	derived := ed25519.NewKeyFromSeed(private[:ed25519.SeedSize])
	if !slices.Equal(derived, private) || !slices.Equal(private[32:], pub) {
		return errors.New("update signing key does not match mygo.json")
	}
	return nil
}
func verifyManifest(cfg config, dir, name string) error {
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return err
	}
	var m manifest
	if err = json.Unmarshal(data, &m); err != nil {
		return err
	}
	if m.Version != cfg.Version {
		return fmt.Errorf("%s has the wrong version", name)
	}
	u, err := url.Parse(m.URL)
	if err != nil {
		return err
	}
	prefix := "https://github.com/" + cfg.Updates.GitHub + "/releases/download/v" + cfg.Version + "/"
	if !strings.HasPrefix(m.URL, prefix) || u.RawQuery != "" || u.Fragment != "" || strings.TrimPrefix(m.URL, prefix) != filepath.Base(u.Path) {
		return errors.New("update manifest has an unexpected asset URL")
	}
	path := filepath.Join(dir, filepath.Base(u.Path))
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	sum := sha256.New()
	size, err := io.Copy(sum, f)
	if err != nil {
		return err
	}
	sig, err := base64.StdEncoding.DecodeString(m.Signature)
	if err != nil {
		return err
	}
	pub, err := base64.StdEncoding.DecodeString(cfg.Updates.PublicKey)
	if err != nil {
		return err
	}
	if size != m.Size || !ed25519.Verify(pub, sum.Sum(nil), sig) {
		return fmt.Errorf("%s failed archive size/signature verification", name)
	}
	return nil
}
func verifyRelease(cfg config, dir string) error {
	files, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	manifests := 0
	lines := []string{}
	for _, file := range files {
		name := file.Name()
		if file.IsDir() || name == "SHA256SUMS.txt" {
			continue
		}
		if strings.HasPrefix(name, "update-") && strings.HasSuffix(name, ".json") {
			if err = verifyManifest(cfg, dir, name); err != nil {
				return err
			}
			manifests++
		}
		f, err := os.Open(filepath.Join(dir, name))
		if err != nil {
			return err
		}
		h := sha256.New()
		_, err = io.Copy(h, f)
		f.Close()
		if err != nil {
			return err
		}
		lines = append(lines, hex.EncodeToString(h.Sum(nil))+"  "+name)
	}
	if manifests == 0 {
		return errors.New("no signed update manifests found")
	}
	if err = os.WriteFile(filepath.Join(dir, "SHA256SUMS.txt"), []byte(strings.Join(lines, "\n")+"\n"), 0644); err != nil {
		return err
	}
	fmt.Printf("Verified %d signed update manifests and %d release assets\n", manifests, len(lines))
	return nil
}
