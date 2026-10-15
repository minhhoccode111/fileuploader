// Package validatorx wraps the go-playground validator with the custom
// validation rules this app needs and a helper that turns a validation failure
// into human-readable messages.
package validatorx

import (
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
)

// Precompiled regexes — compiled once at package init to avoid repeated allocation on every validation call.
//
//nolint:gochecknoglobals // package-level regex vars are intentional: compiled once, read-only after init.
var (
	// reTag: letters (any language), digits, underscores, hyphens, and internal spaces.
	// Must start and end with a letter, digit, underscore, or hyphen — no leading/trailing spaces.
	// Example matches: "sci-fi", "golang 101", "my_tag".
	reTag = regexp.MustCompile(`^[\p{L}0-9_-]([\p{L}0-9_ -]*[\p{L}0-9_-])?$`)

	// reUsername: letters (any language) and digits only. No spaces or special characters.
	// Example matches: "minhhoccode111", "Ψuser42".
	reUsername = regexp.MustCompile(`^[\p{L}0-9]+$`)

	// rePassword: Go's regexp uses RE2, which does not support lookaheads, so each
	// character-class rule is expressed as a separate sub-regex and checked individually.
	// All four must match for the password to be valid.
	rePassword = struct {
		upper, lower, digit, special *regexp.Regexp
	}{
		upper:   regexp.MustCompile(`[A-Z]`),                     // at least one uppercase letter
		lower:   regexp.MustCompile(`[a-z]`),                     // at least one lowercase letter
		digit:   regexp.MustCompile(`\d`),                        // at least one digit
		special: regexp.MustCompile(`[!@#~$%^&*()+|_{}<>?,./-]`), // at least one special character
	}
)

// New creates a new validator instance with custom validations.
func New() *validator.Validate {
	v := validator.New(validator.WithRequiredStructEnabled())

	registerStringRules(v)
	registerPtrRules(v)

	return v
}

// registerStringRules registers the validators that operate on string fields.
func registerStringRules(v *validator.Validate) {
	mustRegister(v, "no_dups_str", func(fl validator.FieldLevel) bool {
		slices, ok := fl.Field().Interface().([]string)
		if !ok {
			return false
		}

		seen := make(map[string]struct{})

		for _, t := range slices {
			t = strings.TrimSpace(t)
			if _, exists := seen[t]; exists {
				return false
			}

			seen[t] = struct{}{}
		}

		return true
	})

	mustRegister(v, "tag", func(fl validator.FieldLevel) bool {
		return reTag.MatchString(fl.Field().String())
	})

	mustRegister(v, "username", func(fl validator.FieldLevel) bool {
		return reUsername.MatchString(fl.Field().String())
	})

	mustRegister(v, "password", func(fl validator.FieldLevel) bool {
		p := fl.Field().String()

		return rePassword.upper.MatchString(p) &&
			rePassword.lower.MatchString(p) &&
			rePassword.digit.MatchString(p) &&
			rePassword.special.MatchString(p)
	})

	mustRegister(v, "date_only", func(fl validator.FieldLevel) bool {
		s := fl.Field().String()
		if s == "" {
			return true
		}

		_, err := time.Parse("2006-01-02", s)

		return err == nil
	})
}

// registerPtrRules registers the validators that delegate to a built-in rule
// after unwrapping a pointer, so a nil pointer is treated as valid.
//
// go-playground/validator dereferences a non-nil pointer before the rule runs,
// so the field arrives already unwrapped; a nil pointer is only delivered when
// the rule is registered with callValidationEvenIfNull.
func registerPtrRules(v *validator.Validate) {
	mustRegister(v, "ptr_min", func(fl validator.FieldLevel) bool {
		field := fl.Field()
		if field.Kind() == reflect.Pointer {
			if field.IsNil() {
				return true
			}

			field = field.Elem()
		}

		return v.Var(field.Interface(), "min="+fl.Param()) == nil
	}, true)

	mustRegister(v, "ptr_max", func(fl validator.FieldLevel) bool {
		field := fl.Field()
		if field.Kind() == reflect.Pointer {
			if field.IsNil() {
				return true
			}

			field = field.Elem()
		}

		return v.Var(field.Interface(), "max="+fl.Param()) == nil
	}, true)
}

// mustRegister registers a custom validation and panics if it fails.
// Failures only occur when the tag name clashes with a built-in validator tag,
// so a panic here indicates a programming error that should be fixed at compile time.
// Pass callEvenIfNil to run the rule for a nil pointer field as well.
func mustRegister(v *validator.Validate, tag string, fn validator.Func, callEvenIfNil ...bool) {
	if err := v.RegisterValidation(tag, fn, callEvenIfNil...); err != nil {
		panic("validatorx: failed to register '" + tag + "': " + err.Error())
	}
}
