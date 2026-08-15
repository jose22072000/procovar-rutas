package drive

import (
	"context"
	"fmt"
	"time"
)

// Fake is an in-memory Drive for tests and for demo mode.
//
// It exercises the whole ingest path — seller resolution, hash deduplication,
// files with no date, download failures — with no credentials, no network and
// without waiting for the real folders to arrive.
type Fake struct {
	// Files by folder id.
	Carpetas map[string][]File
	// Content by file id.
	Content map[string][]byte
	// Failures forces an error when downloading that file, to prove one bad file
	// does not bring down the whole scan.
	Failures map[string]error
	// Calls counts downloads: used to check that an already-processed file is NOT
	// downloaded again.
	Calls int
}

func NewFake() *Fake {
	return &Fake{
		Carpetas: map[string][]File{},
		Content:  map[string][]byte{},
		Failures: map[string]error{},
	}
}

// Add puts a file into a folder.
func (f *Fake) Add(folderID string, fich File, contenido []byte) {
	if fich.Modified.IsZero() {
		fich.Modified = time.Now().UTC()
	}
	if fich.Created.IsZero() {
		fich.Created = fich.Modified
	}
	fich.Size = int64(len(contenido))
	f.Carpetas[folderID] = append(f.Carpetas[folderID], fich)
	f.Content[fich.ID] = contenido
}

func (f *Fake) List(_ context.Context, folderID string, desde time.Time, max int) ([]File, error) {
	out := []File{}
	for _, fich := range f.Carpetas[folderID] {
		if !desde.IsZero() && !fich.Modified.After(desde) {
			continue
		}
		if len(out) >= max {
			break
		}
		out = append(out, fich)
	}
	return out, nil
}

func (f *Fake) Download(_ context.Context, fileID string) ([]byte, error) {
	f.Calls++
	if err, mal := f.Failures[fileID]; mal {
		return nil, err
	}
	datos, ok := f.Content[fileID]
	if !ok {
		return nil, fmt.Errorf("fichero %s no encontrado", fileID)
	}
	return datos, nil
}
