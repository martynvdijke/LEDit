package handlers

import (
	"net/url"
	"strconv"
	"strings"
)

// ValidationError represents a single validation failure.
type ValidationError struct {
	Field   string
	Message string
}

// Validator collects validation errors.
type Validator struct {
	Errors []ValidationError
}

func NewValidator() *Validator {
	return &Validator{}
}

func (v *Validator) Valid() bool {
	return len(v.Errors) == 0
}

func (v *Validator) Error() string {
	if len(v.Errors) == 0 {
		return ""
	}
	msgs := make([]string, len(v.Errors))
	for i, e := range v.Errors {
		msgs[i] = e.Field + ": " + e.Message
	}
	return strings.Join(msgs, "; ")
}

// Required checks that a string value is non-empty after trimming spaces.
func (v *Validator) Required(field, value string) *Validator {
	if strings.TrimSpace(value) == "" {
		v.Errors = append(v.Errors, ValidationError{Field: field, Message: "is required"})
	}
	return v
}

// URL checks that a string is a valid URL (has scheme and host).
func (v *Validator) URL(field, value string) *Validator {
	if value == "" {
		return v // skip empty: use Required for mandatory
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		v.Errors = append(v.Errors, ValidationError{Field: field, Message: "must be a valid URL"})
	}
	return v
}

// RangeInt checks that an integer falls within [min, max].
func (v *Validator) RangeInt(field string, value, min, max int) *Validator {
	if value < min || value > max {
		v.Errors = append(v.Errors, ValidationError{Field: field, Message: "must be between " + strconv.Itoa(min) + " and " + strconv.Itoa(max)})
	}
	return v
}

// RangeFloat checks that a float falls within [min, max].
func (v *Validator) RangeFloat(field string, value, min, max float64) *Validator {
	if value < min || value > max {
		v.Errors = append(v.Errors, ValidationError{Field: field, Message: "must be between " + strconv.FormatFloat(min, 'f', -1, 64) + " and " + strconv.FormatFloat(max, 'f', -1, 64)})
	}
	return v
}

// Port checks that a value is a valid port number (1-65535).
func (v *Validator) Port(field string, value int) *Validator {
	return v.RangeInt(field, value, 1, 65535)
}
