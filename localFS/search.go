package localFS

import (
	"os"
	"path"
	"strings"

	"github.com/ninjamarcus/ninjaStorage/models"
)

type Entry struct {
	directory string
	list      Queue[os.DirEntry]
}

type Stack = Queue[*Entry]

func search(directory string, prefix string) (map[string]*models.FileMetaData, error) {
	list, err := os.ReadDir(directory)
	if err != nil {
		return nil, err
	}
	entry := &Entry{directory: "", list: list}

	result := make(map[string]*models.FileMetaData)
	stack := make(Stack, 0)

	for {
		if entry.list.empty() {
			if stack.empty() {
				break
			}
			entry = stack.pop()
			continue
		}

		file := entry.list.shift()
		name := path.Join(entry.directory, file.Name())

		candidate := strings.HasPrefix(name, prefix)

		if candidate {
			if file.Type().IsRegular() {
				metadata, err := getMetaData(name, path.Join(directory, name))
				if err != nil {
					return nil, err
				}
				result[name] = metadata
				continue
			}
		}

		if candidate || len(name) < len(prefix) {
			if file.IsDir() {
				list, err := os.ReadDir(path.Join(directory, name))
				if err != nil {
					return nil, err
				}
				stack.push(entry)
				entry = &Entry{directory: name, list: list}
			}
		}
	}

	return result, nil
}
