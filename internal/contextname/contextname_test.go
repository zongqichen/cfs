package contextname

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateAcceptsPortableNames(t *testing.T) {
	for _, name := range []string{Default, "prod", "eu12-prod", "poc_2", "team.blue"} {
		if err := Validate(name); err != nil {
			t.Errorf("Validate(%q) error = %v", name, err)
		}
	}
}

func TestValidateRejectsAmbiguousOrUnsafeNames(t *testing.T) {
	for _, name := range []string{"", ".", "..", "Prod", "-prod", "prod-", "prod/eu12", "prod eu12", strings.Repeat("a", MaxLength+1)} {
		err := Validate(name)
		if !errors.Is(err, ErrInvalid) {
			t.Errorf("Validate(%q) error = %v, want ErrInvalid", name, err)
		}
	}
}
