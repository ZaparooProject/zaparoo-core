// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later
//
// This file is part of Zaparoo Core.
//
// Zaparoo Core is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// Zaparoo Core is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.

// Package validation provides validation for API request parameters using
// go-playground/validator with custom validators for Zaparoo-specific types.
package validation

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/go-playground/validator/v10"
)

// Common validation errors.
var (
	ErrMissingParams = errors.New("missing params")
	ErrInvalidParams = errors.New("invalid params")
)

// contextKey is an unexported type for context keys to avoid collisions.
type contextKey struct{}

// validateCtxKey is the context key for Context.
var validateCtxKey = contextKey{}

// Validator handles validation of API parameters.
type Validator struct {
	validate *validator.Validate
}

// Context provides runtime context for validation.
type Context struct {
	LauncherIDs []string
}

// NewContext creates a Context from a list of launcher IDs.
func NewContext(launcherIDs []string) *Context {
	return &Context{LauncherIDs: launcherIDs}
}

// NewValidator creates a new Validator with registered custom validators.
func NewValidator() *Validator {
	v := validator.New(validator.WithRequiredStructEnabled())

	// Register custom validators for types that can't use built-ins
	_ = v.RegisterValidation("letter", validateLetter)
	_ = v.RegisterValidation("duration", validateDuration)
	_ = v.RegisterValidation("regex", validateRegex)
	_ = v.RegisterValidation("system", validateSystem)
	_ = v.RegisterValidation("hexdata", validateHexData)
	_ = v.RegisterValidationCtx("launcher", validateLauncher)

	return &Validator{validate: v}
}

// DefaultValidator is a shared validator instance for API use.
var DefaultValidator = NewValidator()

// Validate validates a struct and returns a formatted error if validation fails.
func (v *Validator) Validate(params any) error {
	return v.ValidateCtx(context.Background(), params, nil)
}

// ValidateCtx validates a struct with context and returns a formatted error.
func (v *Validator) ValidateCtx(ctx context.Context, params any, vctx *Context) error {
	ctxVal := context.WithValue(ctx, validateCtxKey, vctx)
	if err := v.validate.StructCtx(ctxVal, params); err != nil {
		var validationErrors validator.ValidationErrors
		if errors.As(err, &validationErrors) {
			return NewError(validationErrors)
		}
		return fmt.Errorf("validation failed: %w", err)
	}
	return nil
}

// RegisterStructValidation registers a struct-level validation function.
func (v *Validator) RegisterStructValidation(fn validator.StructLevelFunc, types ...any) {
	v.validate.RegisterStructValidation(fn, types...)
}

// ValidateAndUnmarshal unmarshals JSON params and validates them.
// Returns ErrMissingParams if params is empty, ErrInvalidParams if unmarshal fails,
// or an Error if validation fails.
func ValidateAndUnmarshal[T any](params json.RawMessage, dest *T) error {
	return ValidateAndUnmarshalCtx(context.Background(), params, dest, nil)
}

// ValidateAndUnmarshalCtx unmarshals JSON params and validates them with context.
func ValidateAndUnmarshalCtx[T any](ctx context.Context, params json.RawMessage, dest *T, vctx *Context) error {
	if len(params) == 0 {
		return ErrMissingParams
	}
	if err := json.Unmarshal(params, dest); err != nil {
		return ErrInvalidParams
	}
	return DefaultValidator.ValidateCtx(ctx, dest, vctx)
}

// validateLetter checks alphabetic letter format (A-Z, 0-9, #).
func validateLetter(fl validator.FieldLevel) bool {
	val := fl.Field().String()
	if val == "" {
		return true
	}
	upper := strings.ToUpper(strings.TrimSpace(val))
	if upper == "0-9" || upper == "#" {
		return true
	}
	if len(upper) == 1 && upper >= "A" && upper <= "Z" {
		return true
	}
	return false
}

// validateDuration checks if string is a valid Go duration.
func validateDuration(fl validator.FieldLevel) bool {
	val := fl.Field().String()
	if val == "" {
		return true
	}
	_, err := time.ParseDuration(val)
	return err == nil
}

// validateRegex checks if string is a valid regex pattern.
func validateRegex(fl validator.FieldLevel) bool {
	val := fl.Field().String()
	if val == "" {
		return true
	}
	_, err := regexp.Compile(val)
	return err == nil
}

// validateSystem checks if system ID exists in systemdefs.
func validateSystem(fl validator.FieldLevel) bool {
	val := fl.Field().String()
	if val == "" {
		return true
	}
	_, err := systemdefs.LookupSystem(val)
	return err == nil
}

// validateHexData checks if string is valid hex data, allowing spaces between bytes.
// Accepts formats like "AABBCC", "AA BB CC", "aa bb cc", etc.
// After stripping spaces, must be valid hex that decodes to complete bytes.
func validateHexData(fl validator.FieldLevel) bool {
	val := fl.Field().String()
	if val == "" {
		return true
	}
	// Strip spaces (same normalization as HandleRun)
	normalized := strings.ReplaceAll(val, " ", "")
	if normalized == "" {
		return false
	}
	// Must decode to valid hex bytes
	_, err := hex.DecodeString(normalized)
	return err == nil
}

// validateLauncher checks if launcher ID exists (context-aware).
func validateLauncher(ctx context.Context, fl validator.FieldLevel) bool {
	val := fl.Field().String()
	if val == "" {
		return true
	}
	vctx, ok := ctx.Value(validateCtxKey).(*Context)
	if !ok || vctx == nil {
		return true // No context means skip validation
	}
	for _, id := range vctx.LauncherIDs {
		if strings.EqualFold(id, val) {
			return true
		}
	}
	return false
}

// ValidateRegexPattern validates that a regex pattern compiles successfully.
// This is for cases where we need the actual error message from regex compilation.
func ValidateRegexPattern(pattern string) error {
	if pattern == "" {
		return nil
	}
	_, err := regexp.Compile(pattern)
	if err != nil {
		return fmt.Errorf("invalid regex pattern: %w", err)
	}
	return nil
}
