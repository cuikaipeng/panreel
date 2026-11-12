package engineupdate

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

func extract(ctx context.Context, archive, dest, binary string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	seen := map[string]bool{}
	var total int64
	for count := 0; ; count++ {
		if err = ctx.Err(); err != nil {
			return err
		}
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		name := h.Name
		if count > 5000 || name != path.Clean(name) || strings.Contains(name, "\\") || path.IsAbs(name) || strings.HasPrefix(name, "../") || (name != binary && !strings.HasPrefix(name, "licenses/")) || seen[name] {
			return errors.New("invalid engine archive entry")
		}
		seen[name] = true
		if h.Typeflag != tar.TypeReg || h.Size < 0 || h.Size > 256<<20 {
			return errors.New("unsupported engine archive entry")
		}
		total += h.Size
		if total > 512<<20 {
			return errors.New("engine archive is too large")
		}
		filename := filepath.Join(dest, filepath.FromSlash(name))
		if err = os.MkdirAll(filepath.Dir(filename), 0700); err != nil {
			return err
		}
		mode := os.FileMode(0600)
		if name == binary {
			mode = 0700
		}
		out, err := os.OpenFile(filename, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
		if err != nil {
			return err
		}
		_, err = io.CopyN(out, tr, h.Size)
		closeErr := out.Close()
		if err == nil {
			err = closeErr
		}
		if err != nil {
			return err
		}
	}
	if !seen[binary] || !seen["licenses/PanSou-LICENSE.txt"] {
		return errors.New("engine archive is incomplete")
	}
	return nil
}
