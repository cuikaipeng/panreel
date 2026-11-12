package search

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cuikaipeng/panreel/internal/model"
)

func TestSearchUpdatesWaitsForLateResultsWithoutRepeatingSearch(t *testing.T) {
	var starts, polls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Error("missing authentication")
		}
		var req map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		for key, want := range map[string]string{"kw": `"test"`, "src": `"all"`, "channels": `["example"]`, "ext": `{"pages":2}`, "filter": `{"exclude":[],"include":["test"]}`} {
			if string(req[key]) != want {
				t.Errorf("%s changed: %s", key, req[key])
			}
		}
		switch r.URL.Path {
		case "/api/search":
			starts.Add(1)
			if string(req["refresh"]) != "true" {
				t.Error("initial force refresh lost")
			}
			fmt.Fprint(w, `{"data":{"merged_by_type":{"quark":[{"url":"https://pan.quark.cn/s/fast","note":"test fast"}]}}}`)
		case "/api/panreel/search/snapshot":
			poll := polls.Add(1)
			if string(req["refresh"]) != "false" {
				t.Error("poll must not force refresh")
			}
			if poll < 4 {
				// Unchanged counts do not prove that slow sources have finished.
				fmt.Fprint(w, `{"data":{"pending":true,"total":0}}`)
			} else {
				fmt.Fprint(w, `{"data":{"pending":false,"merged_by_type":{"quark":[{"url":"https://pan.quark.cn/s/slow","note":"test slow"}]}}}`)
			}
		default:
			t.Error("unexpected endpoint", r.URL.Path)
		}
	}))
	defer server.Close()
	s := model.Defaults()
	s.Channels, s.Include, s.Extensions = []string{"example"}, "test", []byte(`{"pages":2}`)
	var batches [][]model.Item
	err := NewClient(server.URL, "secret").searchUpdates(context.Background(), "test", s, true, time.Millisecond, func(items []model.Item) {
		batches = append(batches, items)
	})
	if err != nil || starts.Load() != 1 || polls.Load() != 4 || len(batches) != 5 || batches[0][0].Title != "test fast" || batches[4][0].Title != "test slow" {
		t.Fatalf("late results not delivered correctly: starts=%d polls=%d batches=%v err=%v", starts.Load(), polls.Load(), batches, err)
	}
}

func TestSearchUpdatesStopsOnCancelTimeoutAndMalformedProgress(t *testing.T) {
	for _, mode := range []string{"cancel", "timeout", "malformed", "http-error"} {
		t.Run(mode, func(t *testing.T) {
			var starts, polls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				if r.URL.Path == "/api/search" {
					starts.Add(1)
					fmt.Fprint(w, `{"data":{"merged_by_type":{"quark":[{"url":"https://pan.quark.cn/s/fast"}]}}}`)
					return
				}
				polls.Add(1)
				switch mode {
				case "timeout":
					<-r.Context().Done()
				case "malformed":
					fmt.Fprint(w, `{"data":{"total":0}}`)
				case "http-error":
					w.WriteHeader(http.StatusServiceUnavailable)
				default:
					t.Error("poll occurred after cancellation")
				}
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			defer cancel()
			s := model.Defaults()
			s.Channels = []string{"example"}
			var delivered []model.Item
			err := NewClient(server.URL, "secret").searchUpdates(ctx, "test", s, false, time.Millisecond, func(items []model.Item) {
				delivered = MergeItems(delivered, items)
				if mode == "cancel" {
					cancel()
				}
			})
			if err == nil || starts.Load() != 1 || len(delivered) != 1 {
				t.Fatal("lost first results or ignored failure", delivered, err)
			}
			if mode == "cancel" && (!errors.Is(err, context.Canceled) || polls.Load() != 0) {
				t.Fatal("cancellation was ignored", err)
			}
			if mode == "timeout" && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal("deadline was restarted during polling", err)
			}
		})
	}
}

func TestMergeUpdatesKeepsOrderAndEnrichesDuplicateLinks(t *testing.T) {
	existing := []model.Item{{ID: "a", Title: "First", Sources: []string{"plugin:one"}}, {ID: "b", Title: "Second"}}
	incoming := []model.Item{{ID: "b", Title: "Second again"}, {ID: "c", Title: "Late"}, {ID: "a", Password: "1234", Quality: "4K", Sources: []string{"tg:example", "plugin:one"}}, {ID: "c", Year: "2026"}}
	got := MergeItems(existing, incoming)
	if len(got) != 3 || got[0].ID != "a" || got[1].ID != "b" || got[2].ID != "c" || got[2].Year != "2026" || got[0].Password != "1234" || got[0].Quality != "4K" || !slices.Equal(got[0].Sources, []string{"plugin:one", "tg:example"}) {
		t.Fatal("updates reordered, duplicated, or lost metadata", got)
	}
	if existing[0].Password != "" || len(existing[0].Sources) != 1 {
		t.Fatal("merge mutated an earlier snapshot")
	}
	if len(MergeItems(got, nil)) != 3 {
		t.Fatal("empty snapshot erased visible results")
	}
}
