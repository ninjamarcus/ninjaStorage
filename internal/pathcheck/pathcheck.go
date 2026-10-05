// Package pathcheck validates the file paths callers pass to a backend.
package pathcheck

import (
	"fmt"
	"path"
	"strings"

	"github.com/ninjamarcus/ninjaStorage/models"
)

// Validate returns models.ErrInvalidPath unless name is a non-empty, relative,
// slash-separated path that stays inside the parent folder once joined to it:
// no leading "/", no "\" and no ".." element.
func Validate(name string) error {
	switch {
	case name == "":
		return fmt.Errorf("%w: path is empty", models.ErrInvalidPath)
	case path.IsAbs(name):
		return fmt.Errorf("%w: %q is absolute", models.ErrInvalidPath, name)
	case strings.Contains(name, `\`):
		return fmt.Errorf("%w: %q contains a backslash", models.ErrInvalidPath, name)
	}
	for _, elem := range strings.Split(name, "/") {
		if elem == ".." {
			return fmt.Errorf("%w: %q contains \"..\"", models.ErrInvalidPath, name)
		}
	}
	if clean := path.Clean(name); clean == "." {
		return fmt.Errorf("%w: %q names the parent folder itself", models.ErrInvalidPath, name)
	}
	return nil
}
