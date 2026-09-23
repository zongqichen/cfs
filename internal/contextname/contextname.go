package contextname

import (
	"errors"
	"fmt"
)

const (
	Default   = "default"
	MaxLength = 63
)

var ErrInvalid = errors.New("invalid context name")

func Validate(name string) error {
	if name == "" || len(name) > MaxLength {
		return invalid(name)
	}
	for index, character := range name {
		if isLowerLetter(character) || isDigit(character) {
			continue
		}
		if index > 0 && index < len(name)-1 && (character == '-' || character == '_' || character == '.') {
			continue
		}
		return invalid(name)
	}
	return nil
}

func invalid(name string) error {
	return fmt.Errorf("%w %q: use 1-63 lowercase letters, digits, dots, hyphens, or underscores; start and end with a letter or digit", ErrInvalid, name)
}

func isLowerLetter(character rune) bool {
	return character >= 'a' && character <= 'z'
}

func isDigit(character rune) bool {
	return character >= '0' && character <= '9'
}
