package enums

type StorageTypes int

const (
	//Local Disk Storage
	LOCAL StorageTypes = iota
	//GCP Disk Storage
	GCP
)

func (s StorageTypes) String() string {
	switch s {
	case LOCAL:
		return "local"
	case GCP:
		return "gcp"
	}
	return "unknown"
}
