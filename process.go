package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

func commandContext(ctx context.Context, name string, args ...string) (*exec.Cmd, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return nil, fmt.Errorf("find %s executable: %w", name, err)
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return nil, fmt.Errorf("resolve %s executable: %w", name, err)
	}
	if err := validateExecutablePath(path); err != nil {
		return nil, fmt.Errorf("refuse %s executable from an unsafe path: %w", name, err)
	}
	return exec.CommandContext(ctx, path, args...), nil
}

func validateExecutablePath(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("executable is not a regular file")
	}
	if runtime.GOOS == "windows" {
		return nil
	}
	if info.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("executable is writable by other users")
	}

	for directory := filepath.Dir(path); ; directory = filepath.Dir(directory) {
		info, err := os.Stat(directory)
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return fmt.Errorf("%s is not a directory", directory)
		}
		if info.Mode().Perm()&0o022 != 0 && info.Mode()&os.ModeSticky == 0 {
			return fmt.Errorf("directory %s is writable by other users", directory)
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			break
		}
	}
	return nil
}
