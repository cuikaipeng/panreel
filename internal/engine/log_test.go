package engine

import (
	"bytes"
	"github.com/cuikaipeng/panreel/internal/model"
	"strings"
	"testing"
)

func TestPrivateLogRedactsSplitSecretsAndBoundsOutput(t *testing.T) {
	var out bytes.Buffer
	s := model.Defaults()
	s.Proxy = "http://user:proxy-pass@localhost:8080"
	s.PluginEnv = map[string]string{"COOKIE": "cookie-secret"}
	log := newPrivateLog(&out, s, "api-secret")
	for _, part := range []string{"normal co", "okie-sec", "ret api-sec", "ret proxy-pass\n"} {
		if _, err := log.Write([]byte(part)); err != nil {
			t.Fatal(err)
		}
	}
	if got := out.String(); strings.Contains(got, "secret") || strings.Contains(got, "proxy-pass") || !strings.Contains(got, "normal") {
		t.Fatal("sensitive log", got)
	}
	_, _ = log.Write(bytes.Repeat([]byte{'x'}, maxLogLine+10))
	_, _ = log.Write([]byte("cookie-secret\nnext line"))
	_ = log.Flush()
	if !strings.Contains(out.String(), "oversized log line omitted") || !strings.HasSuffix(out.String(), "next line") {
		t.Fatal("oversized line handling")
	}
	line := bytes.Repeat([]byte{'z'}, maxLogLine-1)
	line = append(line, '\n')
	for range 140 {
		_, _ = log.Write(line)
	}
	if out.Len() > maxLogBytes {
		t.Fatal("log grew without bound")
	}
}
