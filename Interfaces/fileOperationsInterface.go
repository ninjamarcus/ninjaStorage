package ninjaStorage

import (
	"context"
	"io"

	"github.com/ninjamarcus/ninjaStorage/models"
)

type FileOperations interface {
	Connect() error
	Write(data []byte, filePath string, metaData *models.FileMetaData) (*models.FileMetaData, error)
	// WriteStream writes r to filePath without holding the whole stream in
	// memory. The file only appears once r has been read to the end and every
	// check in opts has passed. If a check fails, r returns an error, or ctx is
	// cancelled before the write is committed, nothing is stored and an existing
	// file at filePath is left untouched. Cancelling ctx stops the write even if
	// a read from r is blocked; that read finishes in the background, so don't
	// reuse r afterwards.
	//
	// The Md5Hash and Sha256Hash fields of metaData are ignored; use opts to
	// verify the stream. The returned metadata includes the stream's SHA-256.
	WriteStream(ctx context.Context, r io.Reader, filePath string, metaData *models.FileMetaData, opts models.WriteStreamOptions) (*models.FileMetaData, error)
	Delete(filePath string) error
	Move(filePathFrom string, filePathTo string) error
	Copy(filePathFrom string, filePathTo string) error
	//Something to do with searching the metadata
	Find()
	List(prefix string) (map[string]*models.FileMetaData, error)
	Read(filePath string) ([]byte, *models.FileMetaData, error)
}
