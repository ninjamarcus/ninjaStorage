package models

import "errors"

// DefaultChunkSize is the GCS upload chunk size WriteStream uses when
// WriteStreamOptions.ChunkSize is zero. Each in-flight GCS upload holds one
// chunk in memory so that a failed chunk can be retried.
const DefaultChunkSize = 8 * 1024 * 1024

var (
	// ErrChecksumMismatch is returned by WriteStream when the stream does not
	// match WriteStreamOptions.ExpectedSHA256. Nothing is stored.
	ErrChecksumMismatch = errors.New("stream does not match the expected SHA-256")
	// ErrSizeMismatch is returned by WriteStream when the stream length does not
	// match WriteStreamOptions.ExpectedSize. Nothing is stored.
	ErrSizeMismatch = errors.New("stream length does not match the expected size")
	// ErrTooLarge is returned by WriteStream when the stream exceeds
	// WriteStreamOptions.MaxSize. Nothing is stored.
	ErrTooLarge = errors.New("stream exceeds the maximum size")
	// ErrInvalidOptions is returned by WriteStream when the options themselves
	// are unusable, before anything is written.
	ErrInvalidOptions = errors.New("invalid write stream options")
)

// WriteStreamOptions controls a streamed write. The zero value stores the
// stream as-is, with no checks and the default chunk size.
//
// Every check runs before the write is committed: if any fails, the file is
// not created and an existing file at the same path is left untouched.
type WriteStreamOptions struct {
	// ExpectedSHA256 is the hex-encoded SHA-256 of the whole stream. When set,
	// the write is aborted with ErrChecksumMismatch if the stream differs.
	ExpectedSHA256 string
	// ExpectedSize is the exact stream length in bytes. When greater than zero,
	// the write is aborted with ErrSizeMismatch if the stream is shorter or
	// longer.
	ExpectedSize int64
	// MaxSize aborts the write with ErrTooLarge as soon as the stream exceeds
	// this many bytes. Zero means no limit.
	MaxSize int64
	// ChunkSize is the GCS upload chunk size in bytes. Zero uses
	// DefaultChunkSize. Local storage ignores it.
	ChunkSize int
}
