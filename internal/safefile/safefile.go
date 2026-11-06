// Package safefile replaces files atomically and saves timestamped backups
// that never overwrite each other.
package safefile

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// Write atomically replaces path with data: it writes a synced temporary file
// beside the target, sets perm, and renames it into place. A symlink at path
// is followed, so the file it names is replaced and the link is kept.
func Write(path string, data []byte, perm fs.FileMode) error {
	target, err := resolve(path)
	if err != nil {
		return err
	}
	tmp, err := stage(target, data, perm)
	if err != nil {
		return err
	}
	defer os.Remove(tmp)
	return os.Rename(tmp, target)
}

// Create writes data to path like Write, but fails with fs.ErrExist instead
// of replacing a file that already exists there.
func Create(path string, data []byte, perm fs.FileMode) error {
	tmp, err := stage(path, data, perm)
	if err != nil {
		return err
	}
	defer os.Remove(tmp)
	return os.Link(tmp, path)
}

// Mode returns the permissions of the file at path, or fallback when there
// is none, so a rewrite can keep a file's existing mode.
func Mode(path string, fallback fs.FileMode) fs.FileMode {
	if info, err := os.Stat(path); err == nil {
		return info.Mode().Perm()
	}
	return fallback
}

// Backup saves data as a new owner-only file named for path and the current
// time, such as config.toml.2026-10-06--14-30-05.bak, and returns its path.
// It never replaces a file: a name already taken gets a -2, -3, ... suffix.
func Backup(path string, data []byte) (string, error) {
	stamp := path + "." + time.Now().Format("2006-01-02--15-04-05")
	for n := 1; ; n++ {
		backup := stamp + ".bak"
		if n > 1 {
			backup = fmt.Sprintf("%s-%d.bak", stamp, n)
		}
		err := Create(backup, data, 0o600)
		if !errors.Is(err, fs.ErrExist) {
			if err != nil {
				return "", err
			}
			return backup, nil
		}
	}
}

// resolve follows a symlink at path to the file it names.
func resolve(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&fs.ModeSymlink == 0 {
		return path, nil
	}
	target, err := filepath.EvalSymlinks(path)
	if errors.Is(err, fs.ErrNotExist) {
		// A dangling link: write where it points.
		link, err := os.Readlink(path)
		if err != nil {
			return "", err
		}
		if !filepath.IsAbs(link) {
			link = filepath.Join(filepath.Dir(path), link)
		}
		return link, nil
	}
	return target, err
}

// stage writes data to a synced temporary file beside path and returns its
// name.
func stage(path string, data []byte, perm fs.FileMode) (string, error) {
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return "", err
	}
	_, writeErr := f.Write(data)
	if writeErr == nil {
		writeErr = f.Chmod(perm)
	}
	if writeErr == nil {
		writeErr = f.Sync()
	}
	if err := errors.Join(writeErr, f.Close()); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}
