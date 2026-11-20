package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

type jsonBackend struct {
	path string
}

func (j *jsonBackend) Load() (State, error) {
	var st State
	b, err := os.ReadFile(j.path)
	if errors.Is(err, os.ErrNotExist) {
		return st, nil
	}
	if err != nil {
		return st, err
	}
	if err = json.Unmarshal(b, &st); err != nil {
		return State{}, err
	}
	return st, nil
}

func (j *jsonBackend) Save(_, next State) error {
	b, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(j.path), 0700); err != nil {
		return err
	}
	tmp := j.path + ".tmp"
	if err = os.WriteFile(tmp, b, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, j.path)
}

func (j *jsonBackend) Close() error { return nil }
