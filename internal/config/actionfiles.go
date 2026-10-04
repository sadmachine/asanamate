package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/BurntSushi/toml"
)

// ActionFile retains the original bytes so editing cannot silently overwrite
// changes made outside the builder. Name is a basename inside ActionsDir.
type ActionFile struct {
	Name     string
	Action   Action
	Original []byte
}

// LoadActionFiles reads and validates enabled actions in filename order.
func LoadActionFiles(dir string) ([]ActionFile, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.toml"))
	if err != nil {
		return nil, err
	}
	var files []ActionFile
	keys := map[[2]string]string{}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		a := Action{Mode: ModeForeground}
		md, err := toml.Decode(string(data), &a)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if len(md.Undecoded()) > 0 {
			return nil, fmt.Errorf("%s: unknown keys: %v", path, md.Undecoded())
		}
		if err := a.validate(); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		key := [2]string{a.Context, a.Key}
		if other, ok := keys[key]; ok {
			return nil, fmt.Errorf("%s: key %q is already used by %s", path, a.Key, other)
		}
		keys[key] = path
		files = append(files, ActionFile{Name: filepath.Base(path), Action: a, Original: data})
	}
	return files, nil
}

// SaveActionFile validates the whole action set before publishing a complete
// file. New files never overwrite existing paths; edits keep a unique backup.
// It returns actions in the same order used by Load.
func SaveActionFile(dir string, file ActionFile) ([]Action, error) {
	if file.Name == "" || filepath.Base(file.Name) != file.Name || strings.ContainsAny(file.Name, `/\\`) || !strings.HasSuffix(file.Name, ".toml") || file.Name == ".toml" {
		return nil, errors.New("filename must be a basename ending in .toml")
	}
	if err := file.Action.validate(); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	// Coordinate saves from multiple TUI sessions before checking shortcuts
	// and original bytes. External editors are checked by the snapshot below.
	lock, err := os.Open(dir)
	if err != nil {
		return nil, err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return nil, fmt.Errorf("action files are busy; retry saving: %w", err)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	files, err := LoadActionFiles(dir)
	if err != nil {
		return nil, err
	}
	for _, other := range files {
		if other.Name != file.Name && other.Action.Key == file.Action.Key && other.Action.Context == file.Action.Context {
			return nil, fmt.Errorf("key %q is already used by %s", file.Action.Key, other.Name)
		}
	}
	var data bytes.Buffer
	if err := toml.NewEncoder(&data).Encode(file.Action); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, file.Name)
	mode := os.FileMode(0o600)
	if file.Original != nil {
		info, err := os.Lstat(path)
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			return nil, errors.New("cannot edit a non-regular action file")
		}
		mode = info.Mode().Perm()
		current, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		if !bytes.Equal(current, file.Original) {
			return nil, errors.New("action file changed outside builder; reopen it before saving")
		}
		backup, err := os.CreateTemp(dir, file.Name+".*.bak")
		if err != nil {
			return nil, err
		}
		_, writeErr := backup.Write(current)
		if writeErr == nil {
			writeErr = backup.Sync()
		}
		closeErr := backup.Close()
		if err := errors.Join(writeErr, closeErr); err != nil {
			os.Remove(backup.Name())
			return nil, err
		}
	}
	tmp, err := os.CreateTemp(dir, ".action-*")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmp.Name())
	writeErr := tmp.Chmod(mode)
	if writeErr == nil {
		_, writeErr = tmp.Write(data.Bytes())
	}
	if writeErr == nil {
		writeErr = tmp.Sync()
	}
	if err := errors.Join(writeErr, tmp.Close()); err != nil {
		return nil, err
	}
	if file.Original == nil {
		err = os.Link(tmp.Name(), path)
	} else {
		err = os.Rename(tmp.Name(), path)
	}
	if err != nil {
		return nil, err
	}
	return loadActions(dir)
}
