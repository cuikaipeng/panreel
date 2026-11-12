package engine

import (
	"bytes"
	"encoding/json"
	"github.com/cuikaipeng/panreel/internal/model"
	"io"
	"net/url"
	"sort"
	"strings"
	"sync"
)

// Line buffering catches credentials split across process writes. Oversized
// lines are dropped rather than partially emitting a possibly secret fragment.
type privateLog struct {
	mu       sync.Mutex
	dest     io.Writer
	secrets  []string
	pending  []byte
	dropping bool
	written  int
}

const maxLogBytes = 8 << 20
const maxLogLine = 64 << 10

func newPrivateLog(dest io.Writer, s model.Settings, token string) *privateLog {
	values := []string{token, s.Proxy}
	for _, v := range s.PluginEnv {
		if len(v) >= 4 {
			values = append(values, v)
		}
	}
	if u, err := url.Parse(s.Proxy); err == nil && u.User != nil {
		values = append(values, u.User.String())
		if p, ok := u.User.Password(); ok && p != "" {
			values = append(values, p, url.QueryEscape(p))
		}
	}
	var ext any
	if json.Unmarshal(s.Extensions, &ext) == nil {
		var walk func(any)
		walk = func(v any) {
			switch v := v.(type) {
			case string:
				if len(v) >= 4 {
					values = append(values, v)
				}
			case map[string]any:
				for _, x := range v {
					walk(x)
				}
			case []any:
				for _, x := range v {
					walk(x)
				}
			}
		}
		walk(ext)
	}
	sort.Slice(values, func(i, j int) bool { return len(values[i]) > len(values[j]) })
	return &privateLog{dest: dest, secrets: values}
}
func (l *privateLog) emit(line []byte) error {
	text := string(line)
	for _, secret := range l.secrets {
		if secret != "" {
			text = strings.ReplaceAll(text, secret, "[redacted]")
		}
	}
	if l.written+len(text) > maxLogBytes {
		return nil
	}
	n, err := io.WriteString(l.dest, text)
	l.written += n
	return err
}
func (l *privateLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	original := len(p)
	for len(p) > 0 {
		end := bytes.IndexByte(p, '\n')
		count := len(p)
		if end >= 0 {
			count = end + 1
		}
		if !l.dropping {
			if len(l.pending)+count > maxLogLine {
				l.pending = nil
				l.dropping = true
			} else {
				l.pending = append(l.pending, p[:count]...)
			}
		}
		p = p[count:]
		if end >= 0 {
			if l.dropping {
				if err := l.emit([]byte("[oversized log line omitted]\n")); err != nil {
					return 0, err
				}
			} else if err := l.emit(l.pending); err != nil {
				return 0, err
			}
			l.pending = nil
			l.dropping = false
		}
	}
	return original, nil
}
func (l *privateLog) Flush() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.dropping {
		l.pending = nil
		return nil
	}
	err := l.emit(l.pending)
	l.pending = nil
	return err
}
