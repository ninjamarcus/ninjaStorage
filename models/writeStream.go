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
	// ErrInvalidOptions is returned by WriteStream, before anything is written,
	// when the options are unusable or the reader is nil.
	ErrInvalidOptions = errors.New("invalid write stream options")
	// ErrInvalidPath is returned by WriteStream, before anything is written,
	// when the path is empty, absolute, contains a backslash or a ".." element,
	// or names the parent folder itself.
	ErrInvalidPath = errors.New("invalid file path")
)

// WriteStreamOptions controls a streamed write.
//
// Every check is off when its field is zero or empty, so the zero value stores
// the stream as-is, of any size. Callers handling untrusted input should always
// set MaxSize, and should reject a request themselves if they require a
// checksum but none was supplied.
//
// Every check runs before the write is committed: if any fails, the file is
// not created and an existing file at the same path is left untouched.
type WriteStreamOptions struct {
	// ExpectedSHA256 is the hex-encoded SHA-256 of the whole stream, in either
	// case. When set, the write is aborted with ErrChecksumMismatch if the
	// stream differs. Empty means not checked.
	ExpectedSHA256 string
	// ExpectedSize is the exact stream length in bytes. When greater than zero,
	// the write is aborted with ErrSizeMismatch if the stream is shorter or
	// longer. Zero means not checked, so it cannot require an empty stream; use
	// ExpectedSHA256 for that.
	ExpectedSize int64
	// MaxSize aborts the write with ErrTooLarge as soon as the stream exceeds
	// this many bytes, without reading the rest. Zero means no limit.
	MaxSize int64
	// ChunkSize is the GCS upload chunk size in bytes. Zero uses
	// DefaultChunkSize; the Go storage client rounds it up to a multiple of
	// 256 KiB. Local storage ignores it.
	ChunkSize int
}
