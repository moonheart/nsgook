package viewer

import (
	"fmt"
	"path/filepath"
	"strings"
)

// FormatBytes formats a byte count into a human-readable size string (KB, MB, GB, TB).
func FormatBytes(n uint64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := uint64(unit), 0
	for v := n / unit; v >= unit && exp < 4; v /= unit {
		div *= unit
		exp++
	}
	units := []string{"KB", "MB", "GB", "TB", "PB"}
	return fmt.Sprintf("%.2f %s", float64(n)/float64(div), units[exp])
}

// DisplayName returns a friendly file or folder name for path.
func DisplayName(path string) string {
	if path == "" {
		return ""
	}
	cleaned := strings.TrimRight(path, `/\`)
	if cleaned == "" {
		return path
	}
	base := filepath.Base(cleaned)
	if base == "." || base == string(filepath.Separator) || base == "" {
		return path
	}
	return base
}
