package storage

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// LocalBlobStore persists blob files under a configured root path.
type LocalBlobStore struct {
	root string
}

// NewLocalBlobStore creates a file-backed blob store.
func NewLocalBlobStore(root string) *LocalBlobStore {
	return &LocalBlobStore{root: root}
}

// Root returns the blob root path.
func (s *LocalBlobStore) Root() string {
	return s.root
}

// Write writes content to a relative path if it does not already exist.
func (s *LocalBlobStore) Write(relativePath string, content []byte) error {
	fullPath := filepath.Join(s.root, filepath.Clean(relativePath))
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		return fmt.Errorf("create blob directory: %w", err)
	}

	if _, err := os.Stat(fullPath); err == nil {
		return nil
	}

	if err := os.WriteFile(fullPath, content, 0o644); err != nil {
		return fmt.Errorf("write blob: %w", err)
	}
	return nil
}

// Open opens a stored blob using its relative path.
func (s *LocalBlobStore) Open(relativePath string) (io.ReadCloser, error) {
	fullPath := filepath.Join(s.root, filepath.Clean(relativePath))
	file, err := os.Open(fullPath)
	if err != nil {
		return nil, fmt.Errorf("open blob: %w", err)
	}
	return file, nil
}

// Exists reports whether the blob exists on disk.
func (s *LocalBlobStore) Exists(relativePath string) bool {
	fullPath := filepath.Join(s.root, filepath.Clean(relativePath))
	_, err := os.Stat(fullPath)
	return err == nil
}
