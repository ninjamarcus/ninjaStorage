package pathcheck

import (
	"errors"
	"testing"

	"github.com/ninjamarcus/ninjaStorage/models"
)

func TestValidate(t *testing.T) {
	valid := []string{"a", "42/abc.bkup", "a/b/c.tar.gz", "a/./b", "..a", "a..b/c", "dir/"}
	for _, name := range valid {
		if err := Validate(name); err != nil {
			t.Errorf("Validate(%q) = %v, want nil", name, err)
		}
	}

	invalid := []string{"", "/abs", "..", "../x", "a/../../x", "a/..", `a\b`, `..\x`, ".", "./", "a/.."}
	for _, name := range invalid {
		if err := Validate(name); !errors.Is(err, models.ErrInvalidPath) {
			t.Errorf("Validate(%q) = %v, want ErrInvalidPath", name, err)
		}
	}
}
