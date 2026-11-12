package model

import "time"

// ResourceFile is a file in a complete, recursively listed share. Folder names
// and modification times are not used as evidence of new content.
type ResourceFile struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Size int64  `json:"size"`
	Hash string `json:"hash,omitempty"`
}

type WatchResult struct {
	Initialized bool           `json:"initialized"`
	Files       []ResourceFile `json:"files,omitempty"`
	CheckedAt   time.Time      `json:"checked_at"`
	NextCheck   time.Time      `json:"next_check"`
	UpdatedAt   time.Time      `json:"updated_at"`
	Added       int            `json:"added"`
	Changed     int            `json:"changed"`
	Names       []string       `json:"names,omitempty"`
	Issue       string         `json:"issue,omitempty"`
	Failures    int            `json:"failures"`
}

type BookmarkWatch struct {
	ItemID          string
	Enabled         bool
	IntervalMinutes int
	Generation      int64
	ReadAt          time.Time
	Result          WatchResult
}

func (w BookmarkWatch) Unread() bool { return w.Result.UpdatedAt.After(w.ReadAt) }

func ValidWatchInterval(minutes int) bool {
	switch minutes {
	case 30, 60, 180, 360, 720, 1440:
		return true
	}
	return false
}
