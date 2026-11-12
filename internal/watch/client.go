// Package watch reads share directories without downloading or saving files.
package watch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/cuikaipeng/panreel/internal/model"
	"github.com/cuikaipeng/panreel/internal/network"
	"github.com/cuikaipeng/panreel/internal/search"
)

type Issue string

func (e Issue) Error() string { return string(e) }

const (
	Unsupported  Issue = "unsupported"
	Password     Issue = "password"
	Verification Issue = "verification"
	Unavailable  Issue = "unavailable"
	Network      Issue = "network"
	Incomplete   Issue = "incomplete"
	Protocol     Issue = "protocol"
)

func Supported(drive string) bool {
	switch drive {
	case "aliyun", "quark", "uc", "baidu", "115", "123", "tianyi", "xunlei", "pikpak", "guangya", "mobile":
		return true
	}
	return false
}

type Client struct {
	HTTP *http.Client
	// Request spacing also applies within a single large directory scan.
	Spacing  time.Duration
	next     time.Time
	requests int
	bytes    int
}

func Snapshot(ctx context.Context, item model.Item, settings model.Settings) ([]model.ResourceFile, error) {
	transport := network.Transport(settings)
	defer transport.CloseIdleConnections()
	c := &Client{HTTP: &http.Client{Transport: transport, Timeout: 20 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, Spacing: 500 * time.Millisecond}
	return c.Snapshot(ctx, item)
}

type entry struct {
	model.ResourceFile
	dir    bool
	folder string
}
type object map[string]json.RawMessage

func (o object) text(key string) string {
	b := o[key]
	var s string
	if json.Unmarshal(b, &s) == nil {
		return s
	}
	var n json.Number
	if json.Unmarshal(b, &n) == nil {
		return n.String()
	}
	return ""
}
func (o object) number(key string) int64 { n, _ := strconv.ParseInt(o.text(key), 10, 64); return n }
func (o object) obj(key string) object   { var v object; _ = json.Unmarshal(o[key], &v); return v }
func (o object) boolean(key string) bool { return string(o[key]) == "true" || o.text(key) == "1" }
func (o object) list(key string) ([]object, error) {
	var v []object
	if len(o[key]) == 0 || string(o[key]) == "null" || json.Unmarshal(o[key], &v) != nil {
		return nil, Protocol
	}
	return v, nil
}

func issue(code, message string) error {
	s := strings.ToLower(code + " " + message)
	for _, word := range []string{"captcha", "verify", "verification", "login", "unauthorized", "token", "验证", "登录", "频繁", "风控", "risk", "limit"} {
		if strings.Contains(s, word) {
			return Verification
		}
	}
	for _, word := range []string{"password", "passcode", "pass_code", "accesscode", "receive_code", "密码", "提取码", "访问码"} {
		if strings.Contains(s, word) {
			return Password
		}
	}
	for _, word := range []string{"expired", "cancel", "notfound", "not_found", "deleted", "失效", "不存在", "过期", "取消", "删除", "违规"} {
		if strings.Contains(s, word) {
			return Unavailable
		}
	}
	return Protocol
}

// All targets below are fixed provider endpoints. User links are parsed for
// share identifiers only; redirects never forward share passwords or tokens.
func (c *Client) request(ctx context.Context, method, endpoint string, query url.Values, body any, headers map[string]string) (object, error) {
	if c.requests >= 300 {
		return nil, Incomplete
	}
	if delay := time.Until(c.next); delay > 0 {
		t := time.NewTimer(delay)
		defer t.Stop()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-t.C:
		}
	}
	c.next = time.Now().Add(c.Spacing)
	c.requests++
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, Protocol
	}
	if query != nil {
		u.RawQuery = query.Encode()
	}
	var payload []byte
	contentType := "application/json"
	if form, ok := body.(url.Values); ok {
		payload = []byte(form.Encode())
		contentType = "application/x-www-form-urlencoded"
	} else if body != nil {
		payload, err = json.Marshal(body)
		if err != nil {
			return nil, Protocol
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), bytes.NewReader(payload))
	if err != nil {
		return nil, Protocol
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/131.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Cache-Control", "no-cache")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	httpClient := *c.HTTP
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := httpClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, Network
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 || resp.StatusCode == 403 || resp.StatusCode == 429 || (resp.StatusCode >= 300 && resp.StatusCode < 400) {
		return nil, Verification
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, Network
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (8<<20)+1))
	if err != nil {
		return nil, Network
	}
	if len(raw) > 8<<20 {
		return nil, Incomplete
	}
	c.bytes += len(raw)
	if c.bytes > 64<<20 {
		return nil, Incomplete
	}
	if u.Hostname() == "share-kd-njs.yun.139.com" {
		raw, err = openMobile(raw)
		if err != nil {
			return nil, err
		}
	}
	var out object
	if json.Unmarshal(raw, &out) != nil || out == nil {
		return nil, Protocol
	}
	return out, nil
}

func (c *Client) Snapshot(ctx context.Context, item model.Item) ([]model.ResourceFile, error) {
	if !Supported(item.Drive) {
		return nil, Unsupported
	}
	link, _, password, err := search.NormalizeLink(item.URL, item.Drive)
	if err != nil {
		return nil, Protocol
	}
	u, _ := url.Parse(link)
	if item.Password == "" {
		item.Password = password
	}
	if item.Password == "" {
		for _, key := range []string{"code", "receive_code", "pass_code"} {
			if value := u.Query().Get(key); value != "" {
				item.Password = value
				break
			}
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	c.requests = 0
	c.bytes = 0
	switch item.Drive {
	case "aliyun":
		return c.aliyun(ctx, u, item.Password)
	case "quark", "uc":
		return c.quark(ctx, u, item.Password, item.Drive)
	case "baidu":
		return c.baidu(ctx, u, item.Password)
	case "115":
		return c.pan115(ctx, u, item.Password)
	case "123":
		return c.pan123(ctx, u, item.Password)
	case "tianyi":
		return c.tianyi(ctx, u, item.Password)
	case "xunlei", "pikpak":
		return c.thunder(ctx, u, item.Password, item.Drive)
	case "guangya":
		return c.guangya(ctx, u, item.Password)
	case "mobile":
		return c.mobile(ctx, u, item.Password)
	}
	return nil, Unsupported
}

func shareID(u *url.URL) string {
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	for i, p := range parts {
		if (p == "s" || p == "t") && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

func walk(ctx context.Context, root string, list func(string) ([]entry, error)) ([]model.ResourceFile, error) {
	type directory struct {
		id, name string
		depth    int
	}
	queue := []directory{{id: root}}
	visited := map[string]bool{root: true}
	files := make([]model.ResourceFile, 0)
	seen := map[string]bool{}
	for len(queue) > 0 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		d := queue[0]
		queue = queue[1:]
		entries, err := list(d.id)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if e.ID == "" || e.Name == "" || e.Size < 0 {
				return nil, Protocol
			}
			name := path.Join(d.name, e.Name)
			if len(e.ID) > 512 || len(name) > 4096 || len(e.Hash) > 512 {
				return nil, Incomplete
			}
			if e.dir {
				key := e.folder
				if key == "" {
					key = e.ID
				}
				if visited[key] {
					return nil, Incomplete
				}
				if d.depth >= 32 || len(visited) >= 250 {
					return nil, Incomplete
				}
				visited[key] = true
				queue = append(queue, directory{id: key, name: name, depth: d.depth + 1})
			} else {
				if seen[e.ID] {
					return nil, Incomplete
				}
				seen[e.ID] = true
				e.Name = name
				files = append(files, e.ResourceFile)
				if len(files) > 20000 {
					return nil, Incomplete
				}
			}
		}
	}
	return files, nil
}

func failure(err error) string {
	var e Issue
	if errors.As(err, &e) {
		return string(e)
	}
	return string(Network)
}
