package generate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Resolve returns target in resolved form: the longest existing ancestor of the
// path, with the components that do not yet exist appended to it. Every
// containment check runs on this form, because the spelling a caller supplies
// says nothing about where a symlink inside it leads.
//
// A dangling symlink is refused rather than treated as a missing directory:
// creating a file through one writes wherever it points.
func Resolve(target string) (string, error) {
	ancestor := filepath.Clean(target)
	var missing []string
	for {
		resolved, err := filepath.EvalSymlinks(ancestor)
		if err == nil {
			for i := len(missing) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, missing[i])
			}
			return resolved, nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		if _, statErr := os.Lstat(ancestor); statErr == nil {
			return "", fmt.Errorf("path %q contains a dangling symlink", target)
		}
		missing = append(missing, filepath.Base(ancestor))
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			return "", err
		}
		ancestor = parent
	}
}

// Within says whether path is strictly inside directory. Both must already be
// resolved: a prefix test on unresolved paths proves nothing.
func Within(path string, directory string) bool {
	separator := string(filepath.Separator)
	return strings.HasPrefix(path, strings.TrimSuffix(directory, separator)+separator)
}

// Confine resolves a declared runnable-relative path inside dir and refuses a
// target outside it, symlinks included. Core validates the declared spelling,
// but whoever opens the file is what decides which content is trusted.
func Confine(dir string, relative string) (string, error) {
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", fmt.Errorf("resolve runnable directory: %w", err)
	}
	resolved, err := Resolve(filepath.Join(root, relative))
	if err != nil {
		return "", err
	}
	if resolved != root && !Within(resolved, root) {
		return "", fmt.Errorf("path %q resolves outside the runnable directory", relative)
	}
	return resolved, nil
}
