package search

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/cuikaipeng/panreel/internal/model"
)

// SearchUpdates emits the first response and then cache snapshots until the
// engine finishes. One deadline covers the entire search, including updates.
func (c *Client) SearchUpdates(ctx context.Context, query string, s model.Settings, refresh bool, emit func([]model.Item)) error {
	return c.searchUpdates(ctx, query, s, refresh, time.Second, emit)
}

func (c *Client) searchUpdates(ctx context.Context, query string, s model.Settings, refresh bool, interval time.Duration, emit func([]model.Item)) error {
	request, err := searchRequest(query, s, refresh)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(s.Timeout)*time.Second)
	defer cancel()
	data, err := c.call(ctx, "/api/search", request)
	if err != nil {
		return err
	}
	items, err := Normalize(data)
	if err != nil {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	emit(FilterDrives(items, s))
	request["refresh"] = false
	for {
		data, err = c.call(ctx, "/api/panreel/search/snapshot", request)
		if err != nil {
			return err
		}
		var snapshot struct {
			Data struct {
				Pending *bool `json:"pending"`
			} `json:"data"`
		}
		if json.Unmarshal(data, &snapshot) != nil || snapshot.Data.Pending == nil {
			return fmt.Errorf("搜索引擎返回了不完整的进度信息")
		}
		items, err = Normalize(data)
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		emit(FilterDrives(items, s))
		if !*snapshot.Data.Pending {
			return nil
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

// Enforce the user's selection even if an engine snapshot contains extra types.
func FilterDrives(items []model.Item, s model.Settings) []model.Item {
	out := make([]model.Item, 0, len(items))
	for _, item := range items {
		if s.AllowsDrive(item.Drive) {
			out = append(out, item)
		}
	}
	return out
}

// MergeItems keeps existing rows in place, enriches their metadata and appends
// unseen links. A partial/empty snapshot never removes results already shown.
func MergeItems(existing, incoming []model.Item) []model.Item {
	out := slices.Clone(existing)
	index := make(map[string]int, len(out))
	for i, item := range out {
		index[item.ID] = i
	}
	for _, item := range incoming {
		if i, ok := index[item.ID]; ok {
			old := &out[i]
			old.Sources = slices.Clone(old.Sources)
			for _, source := range item.Sources {
				if !slices.Contains(old.Sources, source) {
					old.Sources = append(old.Sources, source)
				}
			}
			if old.Title == "" || old.Title == "未命名分享" {
				old.Title = item.Title
			}
			if len(item.Description) > len(old.Description) {
				old.Description = item.Description
			}
			for _, field := range []struct {
				to   *string
				from string
			}{
				{&old.Password, item.Password}, {&old.Quality, item.Quality},
				{&old.Year, item.Year}, {&old.Size, item.Size}, {&old.Subtitle, item.Subtitle},
			} {
				if *field.to == "" {
					*field.to = field.from
				}
			}
			if old.PublishedAt.IsZero() {
				old.PublishedAt = item.PublishedAt
			}
		} else if len(out) < 10000 {
			index[item.ID] = len(out)
			out = append(out, item)
		}
	}
	return out
}
