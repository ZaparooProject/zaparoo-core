// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
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

package advargs

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/filters"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/go-playground/validator/v10"
	"github.com/go-viper/mapstructure/v2"
)

// contextKey is an unexported type for context keys to avoid collisions.
type contextKey struct{}

// parseCtxKey is the context key for ParseContext.
var parseCtxKey = contextKey{}

// Parser handles parsing and validation of advanced arguments.
type Parser struct {
	validate *validator.Validate
}

// NewParser creates a new Parser with registered custom validators.
func NewParser() *Parser {
	v := validator.New(validator.WithRequiredStructEnabled())

	// Register custom validators - these won't error since tags are valid
	_ = v.RegisterValidationCtx("launcher", validateLauncher)
	_ = v.RegisterValidation("system", validateSystem)

	return &Parser{validate: v}
}

// DefaultParser is a convenience parser for simple use cases.
var DefaultParser = NewParser()

// ParseContext provides runtime context for validation.
type ParseContext struct {
	LauncherIDs []string
}

// NewParseContext creates a ParseContext from a list of launcher IDs.
func NewParseContext(launcherIDs []string) *ParseContext {
	return &ParseContext{LauncherIDs: launcherIDs}
}

// Parse decodes raw advanced args into a typed struct and validates them.
// Returns an error if decoding or validation fails.
func (p *Parser) Parse(raw map[string]string, dest any, ctx *ParseContext) error {
	// Configure mapstructure decoder
	decoderConfig := &mapstructure.DecoderConfig{
		Result:           dest,
		TagName:          "advarg",
		WeaklyTypedInput: true,
		ErrorUnused:      true, // Fail on unknown args (catches typos like "lancher" instead of "launcher")
		DecodeHook: mapstructure.ComposeDecodeHookFunc(
			stringToTagFiltersHook(),
		),
	}

	decoder, err := mapstructure.NewDecoder(decoderConfig)
	if err != nil {
		return fmt.Errorf("failed to create decoder: %w", err)
	}

	if err := decoder.Decode(raw); err != nil {
		return fmt.Errorf("failed to decode arguments: %w", err)
	}

	// Run validation with context
	ctxVal := context.WithValue(context.Background(), parseCtxKey, ctx)
	if err := p.validate.StructCtx(ctxVal, dest); err != nil {
		var validationErrors validator.ValidationErrors
		if errors.As(err, &validationErrors) {
			// Format validation errors nicely
			var errMsgs []string
			for _, fe := range validationErrors {
				errMsgs = append(errMsgs, formatValidationError(fe))
			}
			return fmt.Errorf("invalid arguments: %s", strings.Join(errMsgs, "; "))
		}
		return fmt.Errorf("validation failed: %w", err)
	}

	return nil
}

// Parse is a convenience function using the default parser.
func Parse(raw map[string]string, dest any, ctx *ParseContext) error {
	return DefaultParser.Parse(raw, dest, ctx)
}

// formatValidationError creates a human-readable error message for a validation error.
func formatValidationError(fe validator.FieldError) string {
	field := strings.ToLower(fe.Field())
	switch fe.Tag() {
	case "launcher":
		return fmt.Sprintf("launcher %q not found", fe.Value())
	case "system":
		return fmt.Sprintf("system %q not found", fe.Value())
	case "oneof":
		return fmt.Sprintf("%s must be one of: %s", field, fe.Param())
	default:
		return fmt.Sprintf("%s failed %s validation", field, fe.Tag())
	}
}

// stringToTagFiltersHook converts a string to []zapscript.TagFilter.
func stringToTagFiltersHook() mapstructure.DecodeHookFunc {
	return func(from reflect.Type, to reflect.Type, data any) (any, error) {
		if from.Kind() != reflect.String {
			return data, nil
		}
		if to != reflect.TypeOf([]zapscript.TagFilter{}) {
			return data, nil
		}

		str, ok := data.(string)
		if !ok || str == "" {
			return []zapscript.TagFilter{}, nil
		}

		parts := strings.Split(str, ",")
		tagFilters, err := filters.ParseTagFilters(parts)
		if err != nil {
			return nil, fmt.Errorf("invalid tags format: %w", err)
		}

		return tagFilters, nil
	}
}

// ShouldRun returns true if the command should execute based on the When condition.
func ShouldRun(g zapscript.GlobalArgs) bool {
	if g.When == "" {
		return true
	}
	return helpers.IsTruthy(g.When)
}

// validateLauncher checks if a launcher ID exists in the available launchers.
// Comparison is case-insensitive since launcher IDs are user-facing identifiers.
func validateLauncher(ctx context.Context, fl validator.FieldLevel) bool {
	launcherID := fl.Field().String()
	if launcherID == "" {
		return true // omitempty handles empty case
	}

	parseCtx, ok := ctx.Value(parseCtxKey).(*ParseContext)
	if !ok || parseCtx == nil {
		// No context means we can't validate - allow it through
		// (will fail at runtime if invalid)
		return true
	}

	for _, id := range parseCtx.LauncherIDs {
		if strings.EqualFold(id, launcherID) {
			return true
		}
	}

	return false
}

// validateSystem checks if a system ID is valid.
func validateSystem(fl validator.FieldLevel) bool {
	systemID := fl.Field().String()
	if systemID == "" {
		return true // omitempty handles empty case
	}

	_, err := systemdefs.LookupSystem(systemID)
	return err == nil
}
