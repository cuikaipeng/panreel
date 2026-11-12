package search

import (
	"encoding/json"
	"fmt"
	"html"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/cuikaipeng/panreel/internal/catalog"
	"github.com/cuikaipeng/panreel/internal/model"
)

var tags = regexp.MustCompile(`<[^>]*>`)
var year = regexp.MustCompile(`(?:^|[^0-9])((?:19|20)[0-9]{2})(?:[^0-9]|$)`)
var size = regexp.MustCompile(`(?i)[0-9]+(?:\.[0-9]+)?\s*(?:TB|GB|MB|GiB|MiB)`)
var quality4k = regexp.MustCompile(`(?i)(?:\b4k\b|2160p)`)
var quality1080 = regexp.MustCompile(`(?i)\b1080[pPiI]?\b`)
var quality720 = regexp.MustCompile(`(?i)\b720[pPiI]?\b`)

type rawLink struct {
	URL       string `json:"url"`
	Password  string `json:"password"`
	Note      string `json:"note"`
	WorkTitle string `json:"work_title"`
	Source    string `json:"source"`
	Date      string `json:"datetime"`
	Type      string `json:"type"`
}
type rawResult struct {
	Title   string    `json:"title"`
	Content string    `json:"content"`
	Date    string    `json:"datetime"`
	Channel string    `json:"channel"`
	Links   []rawLink `json:"links"`
}
type payload struct {
	Total   *int                 `json:"total"`
	Merged  map[string][]rawLink `json:"merged_by_type"`
	Results []rawResult          `json:"results"`
}

func clean(s string, max int) string {
	s = strings.Join(strings.Fields(html.UnescapeString(tags.ReplaceAllString(s, " "))), " ")
	if utf8.RuneCountInString(s) > max {
		s = string([]rune(s)[:max])
	}
	return s
}
func Normalize(data []byte) ([]model.Item, error) {
	var envelope struct {
		Code *int            `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(data, &envelope) != nil {
		return nil, fmt.Errorf("搜索引擎返回了无法识别的数据")
	}
	if envelope.Code != nil && *envelope.Code != 0 && *envelope.Code != 200 {
		return nil, fmt.Errorf("搜索引擎返回错误：%d", *envelope.Code)
	}
	if len(envelope.Data) > 0 {
		data = envelope.Data
	}
	var p payload
	if json.Unmarshal(data, &p) != nil || (p.Merged == nil && p.Results == nil && (p.Total == nil || *p.Total != 0)) {
		return nil, fmt.Errorf("搜索引擎返回了不完整的数据")
	}
	items := []model.Item{}
	index := map[string]int{}
	add := func(l rawLink, drive string) {
		if !slices.ContainsFunc(catalog.Drives, func(d catalog.Drive) bool { return d.ID == drive }) {
			drive = "others"
		}
		link, id, password, err := NormalizeLink(l.URL, drive)
		if err != nil {
			return
		}
		if l.Password != "" {
			password = clean(l.Password, 64)
		}
		source := clean(l.Source, 100)
		if source == "" {
			source = "公开分享"
		}
		if i, ok := index[id]; ok {
			if !slices.Contains(items[i].Sources, source) {
				items[i].Sources = append(items[i].Sources, source)
			}
			if items[i].Password == "" {
				items[i].Password = password
			}
			return
		}
		if len(items) >= 10000 {
			return
		}
		title := clean(l.WorkTitle, 200)
		if title == "" {
			title = clean(l.Note, 200)
		}
		if title == "" {
			title = "未命名分享"
		}
		text := clean(title+" "+l.Note, 2000)
		i := model.Item{ID: id, Drive: drive, URL: link, Title: title, Description: clean(l.Note, 1000), Password: password, Sources: []string{source}}
		if date, err := time.Parse(time.RFC3339, l.Date); err == nil && date.Year() >= 1970 && date.Before(time.Now().Add(24*time.Hour)) {
			i.PublishedAt = date
		}
		switch {
		case quality4k.MatchString(text):
			i.Quality = "4K"
		case quality1080.MatchString(text):
			i.Quality = "1080P"
		case quality720.MatchString(text):
			i.Quality = "720P"
		}
		if m := year.FindStringSubmatch(text); len(m) > 1 {
			i.Year = m[1]
		}
		i.Size = size.FindString(text)
		if strings.Contains(text, "中英") || strings.Contains(text, "双语字幕") {
			i.Subtitle = "中英字幕"
		} else if strings.Contains(text, "中字") || strings.Contains(text, "中文字幕") || strings.Contains(text, "简中") || strings.Contains(text, "繁中") {
			i.Subtitle = "中文字幕"
		}
		index[id] = len(items)
		items = append(items, i)
	}
	keys := make([]string, 0, len(p.Merged))
	for k := range p.Merged {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	// Interleave providers so broad searches do not bury a smaller source.
	for row := 0; ; row++ {
		found := false
		for _, d := range keys {
			if row < len(p.Merged[d]) {
				add(p.Merged[d][row], d)
				found = true
			}
		}
		if !found {
			break
		}
	}
	for _, r := range p.Results {
		for _, l := range r.Links {
			if l.Note == "" {
				l.Note = r.Title + " " + r.Content
			}
			if l.Source == "" {
				l.Source = "tg:" + r.Channel
			}
			if l.Date == "" {
				l.Date = r.Date
			}
			add(l, l.Type)
		}
	}
	return items, nil
}
