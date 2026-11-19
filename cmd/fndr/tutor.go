package main

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/ValeryCherneykin/fndr/internal/tui"
)

func runTutor() (err error) {
	dir, err := os.MkdirTemp("", "fndr-tutor-")
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, os.RemoveAll(dir)) }()
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return err
	}
	model, err := tui.NewTutor(root)
	if err != nil {
		return err
	}
	_, err = runModel(model)
	return err
}
