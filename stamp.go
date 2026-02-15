package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// FileStamp is a persisted snapshot of file metadata that can be used to detect
// changes more reliably than naive mtime comparisons.
//
// Inspired by https://apenwarr.ca/log/20181113
type FileStamp struct {
	MTimeUnixNano int64  `json:"mtime_unix_nano"`
	Size          int64  `json:"size"`
	Inode         uint64 `json:"inode"`
	Mode          uint32 `json:"mode"`
	UID           uint32 `json:"uid"`
	GID           uint32 `json:"gid"`
}

func (s FileStamp) Equal(o FileStamp) bool {
	return s.MTimeUnixNano == o.MTimeUnixNano &&
		s.Size == o.Size &&
		s.Inode == o.Inode &&
		s.Mode == o.Mode &&
		s.UID == o.UID &&
		s.GID == o.GID
}

func (s FileStamp) String() string {
	return fmt.Sprintf("mtime=%d size=%d inode=%d mode=%o uid=%d gid=%d",
		s.MTimeUnixNano, s.Size, s.Inode, s.Mode, s.UID, s.GID,
	)
}

// stampCacheEntry pairs a FileStamp with the content digest computed for
// that version of the file. When the stamp still matches the file on disk,
// the digest can be reused without re-hashing the file contents.
type stampCacheEntry struct {
	Stamp  FileStamp `json:"stamp"`
	Digest string    `json:"digest"`
}

// FileStampCache is a persistent, path-keyed cache of (FileStamp, digest)
// pairs. It allows skipping expensive content hashing when a file's metadata
// has not changed since the last hash.
type FileStampCache struct {
	mu      sync.Mutex
	path    string
	entries map[string]stampCacheEntry
	dirty   bool
}

// NewFileStampCache creates a new stamp cache that will be persisted at path.
func NewFileStampCache(path string) *FileStampCache {
	return &FileStampCache{
		path:    path,
		entries: make(map[string]stampCacheEntry),
	}
}

// Load reads the stamp cache from disk. If the file does not exist the cache
// starts empty.
func (c *FileStampCache) Load() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	data, err := os.ReadFile(c.path)
	if err != nil {
		if os.IsNotExist(err) {
			c.entries = make(map[string]stampCacheEntry)
			return nil
		}
		return fmt.Errorf("read stamp cache: %w", err)
	}

	entries := make(map[string]stampCacheEntry)
	if err := json.Unmarshal(data, &entries); err != nil {
		// Corrupt cache – start fresh.
		c.entries = make(map[string]stampCacheEntry)
		return nil
	}
	c.entries = entries
	return nil
}

// Save writes the stamp cache to disk if it was modified since the last load
// or save.
func (c *FileStampCache) Save() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.dirty {
		return nil
	}

	data, err := json.Marshal(c.entries)
	if err != nil {
		return fmt.Errorf("marshal stamp cache: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return fmt.Errorf("create stamp cache dir: %w", err)
	}

	if err := os.WriteFile(c.path, data, 0o644); err != nil {
		return fmt.Errorf("write stamp cache: %w", err)
	}

	c.dirty = false
	return nil
}

// Lookup returns the cached digest for path if the file's current stamp
// matches the cached one. Returns ("", false) on miss.
func (c *FileStampCache) Lookup(path string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.entries[path]
	if !ok {
		return "", false
	}

	current, err := StatStamp(path)
	if err != nil {
		return "", false
	}

	if !entry.Stamp.Equal(current) {
		return "", false
	}

	return entry.Digest, true
}

// Update records a new (stamp, digest) pair for path.
func (c *FileStampCache) Update(path string, digest string) {
	stamp, err := StatStamp(path)
	if err != nil {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries[path] = stampCacheEntry{Stamp: stamp, Digest: digest}
	c.dirty = true
}
