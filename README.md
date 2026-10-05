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

Both implement `FileOperations` from the `Interfaces` package. Depend on the
interface, and choose the backend when your application starts.

Every path you pass is relative to the configured `ParentFolder`, and `/` is
the separator on both backends.

### Google Cloud Storage

```go
store, err := ninjaStorage.NewStorageGCP(&models.GCPFSConfig{
	BucketName: "my-bucket",
	FS:         &models.FS{ParentFolder: "backups/prod"},
})
```

- `BucketName` and `ParentFolder` are required. `ProjectID` isn't used.
- Credentials come from Google's [Application Default Credentials][adc]. In a
  deployment, set `GOOGLE_APPLICATION_CREDENTIALS` to a service account key
  file. Locally, run `gcloud auth application-default login`.
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
| `Write` | Writes a whole byte slice. On GCS, the upload is a single request with a 50-second timeout, so use `WriteStream` for anything large. |
| `WriteStream` | Writes from an `io.Reader` without holding the whole file in memory. Can verify a checksum and size, and stores nothing if a check fails. See [Streaming writes](#streaming-writes). |
| `Read` | Reads a whole file into memory and returns its metadata. |
| `Delete` | Removes a file. |
| `Copy` | Copies a file. On GCS it fails if the destination already exists; locally it overwrites. |
| `Move` | Copies, then deletes the source. |
| `List` | Returns metadata for every file whose path starts with `prefix`, including files in subdirectories. GCS keys are full object names, including `ParentFolder`; local keys are relative to `ParentFolder`. |
| `Connect` | Opens the GCS client; called by the constructor. A no-op for local storage. |
| `Find` | Not implemented yet. It panics. |

### Metadata

`models.FileMetaData` describes a stored file:

| Field | GCS | Local |
| -- | -- | -- |
| `Name`, `Size`, `Updated` | ✓ | ✓ |
| `Md5Hash` (hex) | ✓ | ✓ |
| `Sha256Hash` (hex) | Only from `WriteStream` | Only from `WriteStream` |
| `Bucket`, `TimeCreated` | ✓ | — |
| `UserMetaData` | Stored on the object | Not stored |

To attach your own key/value pairs to an object, pass them in
`UserMetaData` when writing. Local storage accepts the field but ignores it.

## Streaming writes

`WriteStream` is the method to use for large files, or for data arriving from
a network connection. Memory use stays bounded however big the file is:

- **GCS** uploads in chunks of `ChunkSize` (8 MiB by default), holding one
  chunk at a time so a failed chunk can be retried.
- **Local** storage writes to a hidden temporary file next to the target.

Nothing is stored until the reader has been read to the end and every check
has passed. If a check fails, the reader returns an error, or `ctx` is
cancelled:

- **GCS** discards the upload, so no object is created.
- **Local** storage removes the temporary file.

In both cases an existing file at the same path is left untouched.

```go
md, err := store.WriteStream(ctx, req.Body, "42/backup.tar.gz",
	&models.FileMetaData{UserMetaData: map[string]string{"orgID": "42"}},
	models.WriteStreamOptions{
		ExpectedSHA256: req.Header.Get("X-Content-SHA256"), // hex
		MaxSize:        512 << 20,                          // 512 MiB
	})
if err != nil {
	switch {
	case errors.Is(err, models.ErrChecksumMismatch), errors.Is(err, models.ErrSizeMismatch):
		// The data was corrupted or truncated; ask the sender to retry.
	case errors.Is(err, models.ErrTooLarge):
		// Reject the upload.
	default:
		// Storage or network failure.
	}
	return err
}
fmt.Println(md.Sha256Hash, md.Md5Hash, md.Size)
```

### Options

All of `models.WriteStreamOptions` is optional. The zero value stores the
stream as-is.

| Option | Effect |
| -- | -- |
| `ExpectedSHA256` | Hex SHA-256 of the whole stream, in either case. On a mismatch, the write fails with `ErrChecksumMismatch`. |
| `ExpectedSize` | Exact length in bytes. A stream that's shorter or longer fails with `ErrSizeMismatch`. |
| `MaxSize` | The write fails with `ErrTooLarge` as soon as the stream goes over this many bytes, without reading the rest. |
| `ChunkSize` | GCS upload chunk size in bytes. Defaults to `models.DefaultChunkSize` (8 MiB); GCS rounds it up to a multiple of 256 KiB. Local storage ignores it. |

Unusable options, such as a SHA-256 that isn't 64 hex characters or a negative
size, fail with `ErrInvalidOptions` before anything is written.

`WriteStream` has no timeout of its own; set a deadline on `ctx`.

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
They cover single-request and multi-chunk uploads, and check that a failed
write never creates an object.

### Troubleshooting

If you get OAuth2 `invalid_grant` errors when testing locally, refresh your
application default credentials:

```sh
gcloud auth application-default login
```

[This post](https://thornelabs.net/posts/resolve-google-cloud-api-oauth2-cannot-fetch-token-invalid-grant-error/)
explains the cause.

[adc]: https://cloud.google.com/docs/authentication/application-default-credentials
