package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/cuikaipeng/panreel/internal/model"
)

type Client struct {
	Base, Token string
	HTTP        *http.Client
}

func NewClient(base, token string) *Client {
	return &Client{Base: base, Token: token, HTTP: &http.Client{Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func (c *Client) call(ctx context.Context, path string, body any) ([]byte, error) {
	var r io.Reader
	method := "GET"
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		r = bytes.NewReader(data)
		method = "POST"
	}
	req, err := http.NewRequestWithContext(ctx, method, c.Base+path, r)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")
	response, err := c.HTTP.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("暂时无法连接内置搜索引擎")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return nil, fmt.Errorf("搜索引擎返回状态 %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 32*1024*1024+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 32*1024*1024 {
		return nil, fmt.Errorf("搜索结果过多，请使用更具体的关键词")
	}
	return data, nil
}
func searchRequest(query string, s model.Settings, refresh bool) (map[string]any, error) {
	query = strings.TrimSpace(query)
	if query == "" || utf8.RuneCountInString(query) > 100 {
		return nil, fmt.Errorf("请输入 1–100 字的关键词")
	}
	source, err := s.EffectiveSource()
	if err != nil {
		return nil, err
	}
	if err = s.Validate(); err != nil {
		return nil, err
	}
	request := map[string]any{"kw": query, "src": source, "res": "merge", "plugins": s.Plugins, "channels": s.Channels, "cloud_types": s.EnabledDrives(), "conc": s.Concurrency, "refresh": refresh}
	if len(s.Extensions) > 0 {
		request["ext"] = s.Extensions
	}
	if s.Include != "" || s.Exclude != "" {
		request["filter"] = map[string][]string{"include": strings.Fields(s.Include), "exclude": strings.Fields(s.Exclude)}
	}
	return request, nil
}

type LivenessItem struct {
	Name      string `json:"name"`
	Status    string `json:"status"`
	Rounds    int    `json:"rounds"`
	Yielded   int    `json:"yielded"`
	Failed    int    `json:"failed"`
	LastError string `json:"last_error"`
}
type Health struct {
	Status   string   `json:"status"`
	Plugins  []string `json:"plugins"`
	Channels []string `json:"channels"`
	TG       struct {
		Reachable bool   `json:"reachable"`
		Reason    string `json:"reason"`
		Age       *int   `json:"checked_seconds_ago"`
	} `json:"tg"`
	Liveness struct {
		FailingPlugins  []LivenessItem `json:"failing_plugins"`
		ZeroPlugins     []LivenessItem `json:"zero_yield_plugins"`
		DegradedPlugins []LivenessItem `json:"degraded_plugins"`
		FailingChannels []LivenessItem `json:"failing_channels"`
		ZeroChannels    []LivenessItem `json:"zero_yield_channels"`
	} `json:"liveness"`
}

type EngineIdentity struct {
	Protocol      int    `json:"protocol"`
	Revision      string `json:"revision"`
	SearchUpdates bool   `json:"search_updates"`
}

func (c *Client) Identity(ctx context.Context) (EngineIdentity, error) {
	var identity EngineIdentity
	data, err := c.call(ctx, "/api/panreel", nil)
	if err == nil {
		err = json.Unmarshal(data, &identity)
	}
	return identity, err
}

func (c *Client) Health(ctx context.Context) (Health, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var h Health
	data, err := c.call(ctx, "/api/health", nil)
	if err != nil {
		return h, err
	}
	err = json.Unmarshal(data, &h)
	if err == nil && h.Status != "ok" {
		err = fmt.Errorf("搜索引擎尚未就绪")
	}
	return h, err
}
func (c *Client) Check(ctx context.Context, item model.Item) (model.Check, error) {
	if !CanCheck(item.Drive) {
		return model.Check{State: "unsupported", Summary: "此类型暂不支持自动检测", At: time.Now()}, nil
	}
	link, _, _, err := NormalizeLink(item.URL, item.Drive)
	if err != nil {
		return model.Check{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	data, err := c.call(ctx, "/api/check/links", map[string]any{"items": []any{map[string]string{"disk_type": item.Drive, "url": link, "password": item.Password}}})
	if err != nil {
		return model.Check{}, err
	}
	var result struct {
		Results []model.Check `json:"results"`
	}
	if json.Unmarshal(data, &result) != nil || len(result.Results) != 1 {
		return model.Check{}, fmt.Errorf("无法识别链接检测结果")
	}
	check := result.Results[0]
	switch check.State {
	case "ok", "bad", "locked", "unsupported", "uncertain":
	default:
		return model.Check{}, fmt.Errorf("未知的链接状态")
	}
	check.At = time.Now()
	return check, nil
}
