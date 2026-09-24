package retention

import (
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// Sweep deletes regular files under roots whose mtime is older than maxAge.
func Sweep(roots []string, maxAge time.Duration, now time.Time) (deleted int, err error) {
	cutoff := now.Add(-maxAge)
	for _, root := range roots {
		if root == "" {
			continue
		}
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return nil
			}
			if d.IsDir() {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return nil
			}
			if info.ModTime().Before(cutoff) {
				if err := os.Remove(path); err == nil {
					deleted++
				}
			}
			return nil
		})
	}
	return deleted, nil
}
