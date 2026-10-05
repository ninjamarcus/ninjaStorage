# ninjaStorage

A small Go library that gives Google Cloud Storage and the local filesystem
the same file interface, so an application can switch between a bucket in
production and a local directory in development through configuration alone.

```sh
go get github.com/ninjamarcus/ninjaStorage
```

Requires Go 1.23 or later.

## Backends

| Backend | Constructor | Use it for |
| -- | -- | -- |
| Google Cloud Storage | `ninjaStorage.NewStorageGCP(*models.GCPFSConfig)` | Production: files are objects in a bucket |
| Local filesystem | `ninjaStorage.NewStorageLocal(*models.LocalFSConfig)` | Development and tests: files live under a directory |

Both implement `FileOperations`. Depend on the interface, and choose the
backend when your application starts. Its package is also named
`ninjaStorage`, so import it under an alias:

```go
import (
	"github.com/ninjamarcus/ninjaStorage"
	ninjaStorageInterfaces "github.com/ninjamarcus/ninjaStorage/Interfaces"
	"github.com/ninjamarcus/ninjaStorage/models"
)

var store ninjaStorageInterfaces.FileOperations
```

Every path you pass is relative to the configured `ParentFolder`, and `/` is
the separator on both backends. `WriteStream` rejects paths that could escape
`ParentFolder` (see [Paths](#paths)); the other methods don't check, so
validate any path built from untrusted input before passing it to them.

### Google Cloud Storage

```go
store, err := ninjaStorage.NewStorageGCP(&models.GCPFSConfig{
	BucketName: "my-bucket",
	FS:         &models.FS{ParentFolder: "backups/prod"},
})
```

- `BucketName` and `ParentFolder` are required. `ProjectID` isn't used.
- Credentials come from Google's [Application Default Credentials][adc]: for
  example a service account key file named by `GOOGLE_APPLICATION_CREDENTIALS`,
  the workload identity of the GCE, GKE or Cloud Run service you run on, or
  `gcloud auth application-default login` on your own machine.
- `STORAGE_EMULATOR_HOST` points the client at a storage emulator instead of
  Google. The tests use this.

### Local filesystem

```go
store, err := ninjaStorage.NewStorageLocal(&models.LocalFSConfig{
	FS: &models.FS{ParentFolder: "/var/lib/myapp/files"},
})
```

`ParentFolder` must be an absolute path to a directory that already exists.
Subdirectories are created as files are written.

## Operations

```go
type FileOperations interface {
	Connect() error
	Write(data []byte, filePath string, metaData *models.FileMetaData) (*models.FileMetaData, error)
	WriteStream(ctx context.Context, r io.Reader, filePath string, metaData *models.FileMetaData, opts models.WriteStreamOptions) (*models.FileMetaData, error)
	Read(filePath string) ([]byte, *models.FileMetaData, error)
	Delete(filePath string) error
	Move(filePathFrom string, filePathTo string) error
	Copy(filePathFrom string, filePathTo string) error
	List(prefix string) (map[string]*models.FileMetaData, error)
	Find()
}
```

| Method | What it does |
| -- | -- |
| `Write` | Writes a whole byte slice, replacing any existing file. On GCS the upload is a single request with a 50-second timeout, empty `data` is rejected, and user metadata is set by a second request afterwards. Use `WriteStream` for anything large. |
| `WriteStream` | Writes from an `io.Reader` without holding the whole file in memory, replacing any existing file. Can verify a checksum and size, and stores nothing if a check fails. See [Streaming writes](#streaming-writes). |
| `Read` | Reads a whole file into memory and returns its metadata. On GCS it has the same 50-second timeout as `Write`. |
| `Delete` | Removes a file. Locally, a missing file returns an error matching `fs.ErrNotExist`. On GCS, see [Known issues](#known-issues). |
| `Copy` | Copies a file. On GCS it fails if the destination already exists, or if both paths are the same. Locally it overwrites the destination. |
| `Move` | On GCS, copies then deletes the source: it fails if the destination exists, and a failed delete leaves both copies. Locally it renames, replacing any existing destination. |
| `List` | Returns metadata for every file whose path starts with `prefix`, including files in subdirectories. See [Listing](#listing). |
| `Connect` | Creates the GCS client; the constructor calls it. A no-op for local storage. |
| `Find` | Not implemented yet. It panics. |

### Listing

`prefix` is a plain string prefix, not a directory name.

- **GCS** joins it to `ParentFolder` with `path.Join`, which drops a trailing
  `/`. So `List("logs/")` also matches `logs-old/` and `logs.txt`, and
  `List("")` can return objects in sibling folders such as `<ParentFolder>-old/`.
  Keys are full object names, including `ParentFolder`, so strip it before
  passing a key back to another method. The whole listing has a 10-second
  timeout.
- **Local** keys are relative to `ParentFolder` and can be passed straight back.
  Each file is read in full to compute its MD5, so listing many large files is
  slow. Files that `WriteStream` is still writing are skipped.

### Metadata

`models.FileMetaData` describes a stored file:

| Field | GCS | Local |
| -- | -- | -- |
| `Name` | Full object name, including `ParentFolder` | The path you passed |
| `Size`, `Updated` | ✓ | ✓ |
| `Md5Hash` (hex) | ✓ | ✓ |
| `Sha256Hash` (hex) | Only in the metadata `WriteStream` returns | Only in the metadata `WriteStream` returns |
| `Bucket`, `TimeCreated` | ✓ | — |
| `UserMetaData` | Stored on the object | Not stored |

`Sha256Hash` isn't stored anywhere, so `Read` and `List` leave it empty.

To attach your own key/value pairs to an object, pass them in
`UserMetaData` when writing. Local storage accepts the field but ignores it.
The `Md5Hash` and `Sha256Hash` you pass in are ignored; to verify a stream,
use the [options](#options).

## Streaming writes

`WriteStream` is the method to use for large files, or for data arriving from
a network connection. Memory use stays bounded however big the file is:

- **GCS** uploads in chunks of `ChunkSize` (8 MiB by default), holding one
  chunk at a time so a failed chunk can be retried.
- **Local** storage writes to a hidden temporary file next to the target, then
  renames it into place.

Nothing is stored until the reader has been read to the end and every check
has passed. If a check fails, the reader returns an error, or `ctx` is
cancelled before the write is committed:

- **GCS** discards the upload, so no object is created.
- **Local** storage removes the temporary file.

In both cases an existing file at the same path is left untouched. A
successful write replaces it.

On GCS, an error from the final commit doesn't prove the object is absent: if
the response to the last request is lost, GCS may already have stored it. All
the checks had passed by then.

```go
// An empty ExpectedSHA256 turns the check off, so if the checksum is
// mandatory, reject a request without one before calling WriteStream.
sum := req.Header.Get("X-Content-SHA256") // hex
if sum == "" {
	return errMissingChecksum
}
md, err := store.WriteStream(ctx, req.Body, "42/backup.tar.gz",
	&models.FileMetaData{UserMetaData: map[string]string{"orgID": "42"}},
	models.WriteStreamOptions{
		ExpectedSHA256: sum,
		MaxSize:        512 << 20, // 512 MiB; always set this for untrusted input
	})
if err != nil {
	switch {
	case errors.Is(err, models.ErrInvalidOptions), errors.Is(err, models.ErrInvalidPath):
		// Malformed checksum or path: reject the request.
	case errors.Is(err, models.ErrChecksumMismatch), errors.Is(err, models.ErrSizeMismatch):
		// The data was corrupted or truncated: ask the sender to retry.
	case errors.Is(err, models.ErrTooLarge):
		// Reject the upload.
	default:
		// Storage or network failure, or ctx was cancelled.
	}
	return err
}
fmt.Println(md.Sha256Hash, md.Md5Hash, md.Size)
```

### Options

All of `models.WriteStreamOptions` is optional. Each check is off when its
field is zero or empty, so the zero value stores the stream as-is, of any size.
**For untrusted input, always set `MaxSize`.**

| Option | Effect |
| -- | -- |
| `ExpectedSHA256` | Hex SHA-256 of the whole stream, in either case. On a mismatch, the write fails with `ErrChecksumMismatch`. Empty means not checked. |
| `ExpectedSize` | Exact length in bytes. A stream that's shorter or longer fails with `ErrSizeMismatch`. Zero means not checked, so it can't require an empty stream; use `ExpectedSHA256` for that. |
| `MaxSize` | The write fails with `ErrTooLarge` as soon as the stream goes over this many bytes, without reading the rest. |
| `ChunkSize` | GCS upload chunk size in bytes. Defaults to `models.DefaultChunkSize` (8 MiB); the Go storage client rounds it up to a multiple of 256 KiB, so the smallest effective chunk is 256 KiB. Local storage ignores it. |

Unusable options, such as a SHA-256 that isn't 64 hex characters or a negative
size, or a nil reader, fail with `ErrInvalidOptions` before anything is
written.

### Timeouts and cancellation

`WriteStream` has no timeout of its own; set a deadline on `ctx`. Cancelling
`ctx` stops the write promptly even if a read from the reader is blocked, for
example on a sender that has stopped sending. That read is left to finish in
the background and its data is discarded, so don't reuse the reader. For a
network stream, also set a read deadline on the connection (for example
`http.Server.ReadTimeout` or `http.ResponseController.SetReadDeadline`) so the
abandoned read ends too.

### Paths

`WriteStream` fails with `ErrInvalidPath`, before anything is written, if the
path is empty, absolute, contains a `\` or a `..` element, or names
`ParentFolder` itself.

### Local files

- Files are created with mode `0644` less the process umask, as `Write` does.
- The data is synced to disk before the rename. Syncing the directory
  afterwards, so the rename itself survives a crash, is best effort.
- Temporary files are named `.<name>.<random>.writestream.tmp`. A process that
  dies mid-write leaves its temporary file behind. `List` hides these files and
  nothing removes them automatically, so clear out old ones if that can happen.

## Example

`example/example.go` writes, reads, copies, lists and deletes a file on
either backend:

```sh
go run ./example -local   # uses /tmp/backup/dev, which must exist
go run ./example          # uses the bucket named in example.go
```

## Development

```sh
go test ./...
go vet ./...
```

The GCS tests run against a small fake of the Cloud Storage upload API (see
`gcpFS/writeStream_test.go`), so they need no credentials or network access.
Only `WriteStream` has GCS tests. They cover single-request and multi-chunk
uploads, a retried chunk and a failed commit, and check that a failed check,
reader error or cancellation never creates or replaces an object.

## Known issues

These predate `WriteStream` and are still to be fixed:

- GCS `Delete` panics if the object doesn't exist.
- GCS `Read` ignores an error fetching the object's attributes, and can panic.
- GCS `Write` panics if `metaData` is nil.
- Local `Copy` with the same source and destination truncates the file.

### Troubleshooting

If you get OAuth2 `invalid_grant` errors when testing locally, refresh your
application default credentials:

```sh
gcloud auth application-default login
```

[This post](https://thornelabs.net/posts/resolve-google-cloud-api-oauth2-cannot-fetch-token-invalid-grant-error/)
explains the cause.

[adc]: https://cloud.google.com/docs/authentication/application-default-credentials
