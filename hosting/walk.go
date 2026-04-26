package hosting

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
)

// walk returns every regular file under root, recursively, sorted by path.
// The returned Files have Root set to the absolute form of the argument, so
// each File's RelPath is source-relative (see File.RelPath).
//
// walk applies no implicit exclusions. Provider filters are applied after
// discovery.
func walk(root string) ([]*File, error) {
	absRoot, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalidRoot, root)
	}

	var files []*File
	err = filepath.WalkDir(absRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("%w: %s", ErrSymlink, path)
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		f, err := newFile(absRoot, path)
		if err != nil {
			return err
		}
		files = append(files, f)
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(files, func(i, j int) bool { return files[i].RelPath() < files[j].RelPath() })
	return files, nil
}
