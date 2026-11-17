package storage

import (
	"crypto/rand"
	"errors"
	"fmt"
	"os"
)

type photoFile interface {
	Write([]byte) (int, error)
	Sync() error
	Close() error
}

type PhotoStorage struct {
	root *os.Root
}

func NewPhotoStorage(directory string) (*PhotoStorage, error) {
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, fmt.Errorf("create photo directory: %w", err)
	}

	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, fmt.Errorf("open photo directory: %w", err)
	}

	return &PhotoStorage{root: root}, nil
}

func (storage *PhotoStorage) Save(data []byte) (string, error) {
	filename := rand.Text()
	file, err := storage.root.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", fmt.Errorf("create photo file: %w", err)
	}

	if err := writePhoto(file, data); err != nil {
		removeErr := storage.root.Remove(filename)
		return "", fmt.Errorf("write photo file: %w", errors.Join(err, removeErr))
	}

	return filename, nil
}

func (storage *PhotoStorage) Read(filename string) ([]byte, error) {
	data, err := storage.root.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("read photo file: %w", err)
	}

	return data, nil
}

func (storage *PhotoStorage) Delete(filename string) error {
	if err := storage.root.Remove(filename); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("delete photo file: %w", err)
	}

	return nil
}

func (storage *PhotoStorage) Close() error {
	return storage.root.Close()
}

func writePhoto(file photoFile, data []byte) error {
	_, err := file.Write(data)
	if err == nil {
		err = file.Sync()
	}

	return errors.Join(err, file.Close())
}
