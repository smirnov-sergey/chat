package storage

import (
	"bytes"
	"errors"
	"slices"
	"testing"
)

type fakePhotoFile struct {
	writeErr error
	syncErr  error
	closeErr error
	data     []byte
	calls    []string
}

func (file *fakePhotoFile) Write(data []byte) (int, error) {
	file.calls = append(file.calls, "write")
	file.data = bytes.Clone(data)
	if file.writeErr != nil {
		return 0, file.writeErr
	}
	return len(data), nil
}

func (file *fakePhotoFile) Sync() error {
	file.calls = append(file.calls, "sync")
	return file.syncErr
}

func (file *fakePhotoFile) Close() error {
	file.calls = append(file.calls, "close")
	return file.closeErr
}

var _ photoFile = (*fakePhotoFile)(nil)

func TestWritePhoto(t *testing.T) {
	writeErr := errors.New("write failed")
	syncErr := errors.New("sync failed")
	closeErr := errors.New("close failed")
	for _, tt := range []struct {
		name     string
		writeErr error
		syncErr  error
		closeErr error
		calls    []string
	}{
		{"success", nil, nil, nil, []string{"write", "sync", "close"}},
		{"write failure skips sync but closes file", writeErr, nil, nil, []string{"write", "close"}},
		{"sync failure closes file", nil, syncErr, nil, []string{"write", "sync", "close"}},
		{"close failure", nil, nil, closeErr, []string{"write", "sync", "close"}},
		{"write and close failures", writeErr, nil, closeErr, []string{"write", "close"}},
		{"sync and close failures", nil, syncErr, closeErr, []string{"write", "sync", "close"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			file := &fakePhotoFile{writeErr: tt.writeErr, syncErr: tt.syncErr, closeErr: tt.closeErr}
			data := []byte("prepared photo")
			err := writePhoto(file, data)
			if !slices.Equal(file.calls, tt.calls) || !bytes.Equal(file.data, data) {
				t.Fatalf("calls=%v, data=%q; want calls=%v and unchanged data", file.calls, file.data, tt.calls)
			}
			if tt.writeErr == nil && tt.syncErr == nil && tt.closeErr == nil && err != nil {
				t.Fatalf("successful save error = %v, want nil", err)
			}
			for _, want := range []error{tt.writeErr, tt.syncErr, tt.closeErr} {
				if want != nil && !errors.Is(err, want) {
					t.Errorf("error=%v must preserve %v", err, want)
				}
			}
		})
	}
}
