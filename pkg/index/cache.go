package index

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/odvcencio/canopy/pkg/model"
)

func Save(path string, idx *model.Index) error {
	if idx == nil {
		return nil
	}

	path = filepath.Clean(path)
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}

	file, err := os.CreateTemp(directory, filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tempPath := file.Name()
	success := false
	defer func() {
		_ = file.Close()
		if !success {
			_ = os.Remove(tempPath)
		}
	}()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(idx); err != nil {
		return err
	}
	if err := file.Chmod(0o644); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		return err
	}
	success = true
	return nil
}

func Load(path string) (*model.Index, error) {
	return loadIndex(path, false)
}

// LoadLenient loads a cached index, accepting older schema versions. Useful
// for analysis commands that can tolerate missing fields rather than OOM'ing
// on a full rebuild.
func LoadLenient(path string) (*model.Index, error) {
	return loadIndex(path, true)
}

func loadIndex(path string, lenient bool) (*model.Index, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var idx model.Index
	if err := json.NewDecoder(file).Decode(&idx); err != nil {
		return nil, err
	}
	if idx.Version != "" && idx.Version != schemaVersion {
		if !lenient {
			return nil, fmt.Errorf("index schema version mismatch: cache has %q, expected %q", idx.Version, schemaVersion)
		}
		fmt.Fprintf(os.Stderr, "index: cache schema %q != expected %q, using anyway (rebuild with 'canopy index build' for full features)\n",
			idx.Version, schemaVersion)
	}
	return &idx, nil
}
