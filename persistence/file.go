// Package persistence provides simple JSON-file persistence for the in-memory
// stores. Each store owns a JSONFile pointed at a path under data/; it loads
// once on startup and saves the whole file after every change.
package persistence

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// JSONFile reads and writes a single JSON document at a fixed path.
type JSONFile struct {
	path string
}

// NewJSONFile returns a JSONFile for path (e.g. "data/users.json"), creating the
// parent directory if needed.
func NewJSONFile(path string) (*JSONFile, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	return &JSONFile{path: path}, nil
}

// Load decodes the file's JSON into v. A missing file is not an error: v is left
// as-is so the caller keeps its zero/empty value (e.g. an empty map).
func (f *JSONFile) Load(v any) error {
	data, err := os.ReadFile(f.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read %s: %w", f.path, err)
	}
	if len(data) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("decode %s: %w", f.path, err)
	}
	return nil
}

// Save writes v as indented JSON. It writes to a temp file and renames it over
// the target so a crash mid-write can't leave a half-written (corrupt) file.
func (f *JSONFile) Save(v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", f.path, err)
	}

	tmp := f.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, f.path); err != nil {
		return fmt.Errorf("rename %s -> %s: %w", tmp, f.path, err)
	}
	return nil
}
