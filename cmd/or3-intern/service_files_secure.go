package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func secureServiceRelativePath(rel string) (string, error) {
	clean := filepath.Clean(strings.TrimSpace(rel))
	if clean == "." {
		return clean, nil
	}
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes root")
	}
	return clean, nil
}

func secureOpenServicePath(rootPath, rel string, flags int, mode os.FileMode) (*os.File, error) {
	clean, err := secureServiceRelativePath(rel)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return root.OpenFile(clean, flags, mode)
}

func secureServiceParent(rootPath, rel string) (*os.Root, string, error) {
	clean, err := secureServiceRelativePath(rel)
	if err != nil {
		return nil, "", err
	}
	if clean == "." {
		return nil, "", fmt.Errorf("operation requires a child path")
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return nil, "", err
	}
	parent, err := root.OpenRoot(filepath.Dir(clean))
	_ = root.Close()
	if err != nil {
		return nil, "", err
	}
	return parent, filepath.Base(clean), nil
}

func syncServiceDirectory(root *os.Root) error {
	// Windows durably journals renames but does not consistently permit
	// FlushFileBuffers on directory handles. Unix requires the directory sync.
	if runtime.GOOS == "windows" {
		return nil
	}
	dir, err := root.Open(".")
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func secureAtomicWriteServiceFile(rootPath, rel string, data []byte, mode os.FileMode) (os.FileInfo, error) {
	parent, name, err := secureServiceParent(rootPath, rel)
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		return nil, err
	}
	tmpName := ".or3-write-" + hex.EncodeToString(random[:])
	tmp, err := parent.OpenFile(tmpName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode.Perm())
	if err != nil {
		return nil, err
	}
	cleanup := true
	defer func() {
		_ = tmp.Close()
		if cleanup {
			_ = parent.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		return nil, err
	}
	if err := tmp.Sync(); err != nil {
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		return nil, err
	}
	if err := parent.Rename(tmpName, name); err != nil {
		return nil, err
	}
	cleanup = false
	if err := syncServiceDirectory(parent); err != nil {
		return nil, err
	}
	result, err := parent.Open(name)
	if err != nil {
		return nil, err
	}
	defer result.Close()
	return result.Stat()
}

func secureCreateServiceFile(rootPath, rel string, source io.Reader, mode os.FileMode) (os.FileInfo, error) {
	parent, name, err := secureServiceParent(rootPath, rel)
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	file, err := parent.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode.Perm())
	if err != nil {
		return nil, err
	}
	complete := false
	defer func() {
		_ = file.Close()
		if !complete {
			_ = parent.Remove(name)
		}
	}()
	if _, err := io.Copy(file, source); err != nil {
		return nil, err
	}
	if err := file.Sync(); err != nil {
		return nil, err
	}
	if err := file.Close(); err != nil {
		return nil, err
	}
	if err := syncServiceDirectory(parent); err != nil {
		return nil, err
	}
	complete = true
	opened, err := parent.Open(name)
	if err != nil {
		return nil, err
	}
	defer opened.Close()
	return opened.Stat()
}

func secureMkdirServicePath(rootPath, rel string, mode os.FileMode) error {
	parent, name, err := secureServiceParent(rootPath, rel)
	if err != nil {
		return err
	}
	defer parent.Close()
	if err := parent.Mkdir(name, mode.Perm()); err != nil {
		return err
	}
	return syncServiceDirectory(parent)
}
