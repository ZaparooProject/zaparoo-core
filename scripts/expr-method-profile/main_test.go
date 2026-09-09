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

package main

import (
	"encoding/json"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGenerateProfileRoundTrip(t *testing.T) {
	t.Parallel()

	root, candidate := t.TempDir(), t.TempDir()
	source := filepath.Join(candidate, "builtin.go")
	require.NoError(t, os.WriteFile(source, []byte("package expr\nconst Version = 1\n"), 0o600))

	// Compare the complete reflected inventory with the reviewed manifest, not
	// only the newly generated output with itself.
	reviewed, err := os.ReadFile("profile.json")
	require.NoError(t, err)
	var expected profile
	require.NoError(t, json.Unmarshal(reviewed, &expected))
	expected.ExprSourceSHA256, err = sourceDigest(candidate)
	require.NoError(t, err)
	require.NoError(t, generateProfile(false, candidate, root, expected.GoZapScriptVersion))

	manifestPath := filepath.Join(root, "scripts", "expr-method-profile", "profile.json")
	// #nosec G304 -- Generated artifact inside t.TempDir.
	manifest, err := os.ReadFile(manifestPath)
	require.NoError(t, err)
	var actual profile
	require.NoError(t, json.Unmarshal(manifest, &actual))
	require.Equal(t, expected, actual)

	artifacts := make([]string, 1, 3)
	artifacts[0] = manifestPath
	for _, name := range []string{"methods_static.go", "upgrade_static_test.go"} {
		path := filepath.Join(candidate, "internal", "staticmethod", name)
		_, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, parser.AllErrors)
		require.NoError(t, parseErr)
		artifacts = append(artifacts, path)
	}
	require.NoError(t, generateProfile(true, candidate, root, expected.GoZapScriptVersion))
	for _, path := range artifacts {
		// #nosec G304 -- Generated artifact inside t.TempDir.
		original, readErr := os.ReadFile(path)
		require.NoError(t, readErr)
		require.NoError(t, os.WriteFile(path, []byte("tampered"), 0o600))
		require.ErrorContains(t, generateProfile(true, candidate, root, expected.GoZapScriptVersion), "profile drift")
		// #nosec G304 -- Generated artifact inside t.TempDir.
		unchanged, readErr := os.ReadFile(path)
		require.NoError(t, readErr)
		require.Equal(t, "tampered", string(unchanged), "check mode must not repair drift")
		// #nosec G703 -- Both path and restored artifact belong to this test's temporary directory.
		require.NoError(t, os.WriteFile(path, original, 0o600))
	}

	require.ErrorContains(t, generateProfile(true, candidate, root, "changed-version"), "profile drift")
	require.NoError(t, os.WriteFile(source, []byte("package expr\nconst Version = 2\n"), 0o600))
	require.ErrorContains(t, generateProfile(true, candidate, root, expected.GoZapScriptVersion), "profile drift")
}

func TestGenerateProfileRejectsMissingSource(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	err := generateProfile(false, filepath.Join(root, "missing"), root, "test")
	require.ErrorContains(t, err, "open Expr source root")
}

func TestEnvironmentGateRejectsUnboundedValues(t *testing.T) {
	values := []any{
		struct{ Value any }{},
		struct{ Value map[string]string }{},
		struct{ Value func() string }{},
	}
	for _, value := range values {
		var schema []string
		if err := environmentSchema(reflect.TypeOf(value), "env", &schema); err == nil {
			t.Fatalf("accepted unaudited environment %T", value)
		}
	}
	var schema []string
	if err := environmentSchema(reflect.TypeOf(struct{ Value string }{}), "env", &schema); err != nil {
		t.Fatal(err)
	}
}

func TestCheckRejectsDriftWithoutRewriting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile.json")
	if err := output(path, []byte("reviewed"), false); err != nil {
		t.Fatal(err)
	}
	if err := output(path, []byte("reviewed"), true); err != nil {
		t.Fatal(err)
	}
	if err := output(path, []byte("changed"), true); err == nil {
		t.Fatal("profile drift was accepted")
	}
	// #nosec G304 -- Path is inside t.TempDir.
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "reviewed" {
		t.Fatal("check rewrote reviewed profile")
	}
}

func TestGenerationRequiresExplicitCheckout(t *testing.T) {
	if err := run(false, ""); err == nil {
		t.Fatal("generation accepted implicit module-cache destination")
	}
}

func TestSourceGateIgnoresNestedModules(t *testing.T) {
	root := t.TempDir()
	before, err := sourceDigest(root)
	if err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "nested")
	if err = os.Mkdir(nested, 0o750); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(nested, "go.mod"), []byte("module nested\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(nested, "nested.go"), []byte("package nested\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	after, err := sourceDigest(root)
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatal("Git-only nested module changed module-archive fingerprint")
	}
}

func TestSourceGateDetectsChange(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "builtin.go")
	if err := os.WriteFile(path, []byte("package example\nconst Version = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := sourceDigest(root)
	if err != nil {
		t.Fatal(err)
	}
	if writeErr := os.WriteFile(path, []byte("package example\nconst Version = 2\n"), 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
	after, err := sourceDigest(root)
	if err != nil {
		t.Fatal(err)
	}
	if before == after {
		t.Fatal("Expr source change not detected")
	}
}
