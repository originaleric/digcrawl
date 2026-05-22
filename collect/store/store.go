package store

import (
	"context"
)

// CollectRecord is one row in the collects table.
type CollectRecord struct {
	ID           string
	SourceURL    string
	CanonicalURL string
	Platform     string
	ToolName     string
	Title        string
	ContentType  string
	Status       string
	ErrorCode    string
	CapturedAt   string
	OutputRoot   string
	BaseName     string
	CreatedAt    string
}

// FileRecord is one exported file for a collect.
type FileRecord struct {
	Kind string
	Path string
}

// MediaRecord is one media asset for a collect.
type MediaRecord struct {
	MediaType     string
	RemoteURL     string
	LocalPath     string
	DownloadError string
	SortOrder     int
}

// CollectDetail is a collect with related files and media.
type CollectDetail struct {
	Collect CollectRecord
	Files   []FileRecord
	Media   []MediaRecord
}

// UpsertInput is written after a successful collect export.
type UpsertInput struct {
	SourceURL    string
	CanonicalURL string
	Platform     string
	ToolName     string
	Title        string
	ContentType  string
	Status       string
	ErrorCode    string
	CapturedAt   string
	OutputRoot   string
	BaseName     string
	Files        []FileRecord
	Media        []MediaRecord
}

// Store persists collect metadata in SQLite.
type Store interface {
	Upsert(ctx context.Context, in UpsertInput) (string, error)
	List(ctx context.Context, limit, offset int) ([]CollectRecord, error)
	GetByID(ctx context.Context, id string) (*CollectDetail, error)
	GetByURL(ctx context.Context, sourceURL string) (*CollectDetail, error)
	Search(ctx context.Context, query string, limit int) ([]CollectRecord, error)
	Close() error
}
