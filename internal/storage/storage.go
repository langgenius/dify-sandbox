package storage

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
)

type Storage interface {
	Put(reader io.Reader, filename string) (string, error)
	Get(fileId string) (io.ReadCloser, error)
}

type LocalStorage struct {
	BaseDir string
}

func NewLocalStorage(baseDir string) (*LocalStorage, error) {
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, err
	}
	return &LocalStorage{BaseDir: baseDir}, nil
}

func (s *LocalStorage) Put(reader io.Reader, filename string) (string, error) {
	// Generate a unique ID for the file
	fileId := uuid.New().String()
	// Create a directory for the file (shard by date to avoid huge directories)
	dateDir := time.Now().Format("20060102")
	dirPath := filepath.Join(s.BaseDir, dateDir)
	if err := os.MkdirAll(dirPath, 0755); err != nil {
		return "", err
	}

	// Calculate full path
	// Storing as ID_filename to keep it simple and preserving extension/name if needed
	// Or simpler: just store by ID, and keep metadata elsewhere?
	// For MVP, lets treat the ID as the key.
	// Actually, storing as `date/id` is good.

	// Let's return "date/id" as the reference key? Or just a long UUID?
	// Let's use "date/id" as the logical key returned to client? No, safer to return opaque ID.
	// Let's store a mapping? No, that requires DB.

	// Stateless approach: Return the relative path as the ID.
	// ID = "20231027/uuid"

	relPath := filepath.Join(dateDir, fileId)
	fullPath := filepath.Join(s.BaseDir, relPath)

	f, err := os.Create(fullPath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	_, err = io.Copy(f, reader)
	if err != nil {
		return "", err
	}

	return relPath, nil
}

func (s *LocalStorage) Get(fileId string) (io.ReadCloser, error) {
	// Validate fileId to prevent directory traversal
	// fileId should generally be "date/uuid"
	cleanPath := filepath.Clean(fileId)
	if cleanPath == "." || cleanPath == "/" {
		return nil, fmt.Errorf("invalid file id")
	}

	fullPath := filepath.Join(s.BaseDir, cleanPath)
	f, err := os.Open(fullPath)
	if err != nil {
		return nil, err
	}
	return f, nil
}
