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

// Expr-method-profile checks the bounded Expr method surface before builds.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"go/format"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
	"time"

	gozapscript "github.com/ZaparooProject/go-zapscript"
	"github.com/expr-lang/expr/builtin"
	"github.com/rs/zerolog/log"
)

type profile struct {
	GoVersion          string
	GoZapScriptVersion string
	ExprSourceSHA256   string
	BuiltinNames       []string
	EnvironmentSchema  []string
	MethodNames        []string
	MethodTypes        []string
}

func main() {
	check := flag.Bool("check", true, "verify generated files without rewriting them")
	exprDir := flag.String("expr-dir", "", "Expr source directory to validate or generate")
	flag.Parse()
	if err := run(*check, *exprDir); err != nil {
		log.Fatal().Err(err).Msg("static method profile validation")
	}
}

func run(check bool, candidate string) error {
	root, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve Core root: %w", err)
	}
	if candidate == "" {
		if !check {
			return errors.New("generation requires -expr-dir pointing to a writable fork checkout")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		data, resolveErr := exec.CommandContext(ctx, "go", "list", "-m", "-f", "{{.Dir}}", "github.com/expr-lang/expr").Output()
		if resolveErr != nil {
			return fmt.Errorf("resolve Expr module: %w", resolveErr)
		}
		candidate = strings.TrimSpace(string(data))
		if candidate == "" {
			return errors.New("expr module directory is empty")
		}
	}
	p := profile{GoVersion: runtime.Version()}
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, dep := range info.Deps {
			if dep.Path == "github.com/ZaparooProject/go-zapscript" {
				p.GoZapScriptVersion = dep.Version
			}
		}
	}
	if p.GoZapScriptVersion == "" {
		return errors.New("cannot identify go-zapscript version")
	}
	p.ExprSourceSHA256, err = sourceDigest(candidate)
	if err != nil {
		return err
	}
	seen := map[reflect.Type]bool{}
	names := map[string]bool{}
	var visit func(reflect.Type)
	visit = func(t reflect.Type) {
		if t == nil || seen[t] {
			return
		}
		seen[t] = true
		for i := range t.NumMethod() {
			m := t.Method(i)
			if m.IsExported() {
				names[m.Name] = true
				visit(m.Type)
			}
		}
		switch t.Kind() {
		case reflect.Array, reflect.Slice, reflect.Pointer, reflect.Chan:
			visit(t.Elem())
		case reflect.Map:
			visit(t.Key())
			visit(t.Elem())
		case reflect.Struct:
			for i := range t.NumField() {
				if t.Field(i).IsExported() {
					visit(t.Field(i).Type)
				}
			}
		case reflect.Invalid, reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
			reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
			reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128, reflect.Interface,
			reflect.String, reflect.UnsafePointer:
			// No concrete child types beyond the method set already visited.
		case reflect.Func:
			for i := range t.NumIn() {
				visit(t.In(i))
			}
			for i := range t.NumOut() {
				visit(t.Out(i))
			}
		}
	}
	for _, t := range []reflect.Type{reflect.TypeFor[gozapscript.ArgExprEnv](), reflect.TypeFor[gozapscript.CustomLauncherExprEnv]()} {
		if schemaErr := environmentSchema(t, t.String(), &p.EnvironmentSchema); schemaErr != nil {
			return schemaErr
		}
		visit(t)
	}
	// now/date have custom validators, so seed their concrete result type.
	// Also cover pointer method sets conservatively.
	for _, t := range []reflect.Type{
		reflect.TypeFor[time.Time](), reflect.TypeFor[*time.Time](),
		reflect.TypeFor[*time.Duration](), reflect.TypeFor[*time.Month](), reflect.TypeFor[*time.Weekday](),
		reflect.TypeFor[*time.Location](),
	} {
		visit(t)
	}
	for _, b := range builtin.Builtins {
		p.BuiltinNames = append(p.BuiltinNames, b.Name)
		for _, t := range b.Types {
			visit(t)
		}
	}
	for name := range names {
		p.MethodNames = append(p.MethodNames, name)
	}
	for t := range seen {
		if t.NumMethod() > 0 {
			p.MethodTypes = append(p.MethodTypes, t.String())
		}
	}
	sort.Strings(p.MethodNames)
	sort.Strings(p.MethodTypes)
	sort.Strings(p.BuiltinNames)
	sort.Strings(p.EnvironmentSchema)
	var out bytes.Buffer
	fmt.Fprintln(&out, "//go:build expr_static_methods\n\n// Code generated by the Core method-profile generator. DO NOT EDIT.\npackage staticmethod\nimport (\"reflect\"; \"fmt\")\nconst Enabled = true")
	fmt.Fprintln(&out, "var methodNames = [...]string{")
	for _, name := range p.MethodNames {
		fmt.Fprintf(&out, "%q,\n", name)
	}
	fmt.Fprintln(&out, "}")
	fmt.Fprintln(&out, "func TypeByName(t reflect.Type, name string) (reflect.Method, bool) { switch name {")
	for _, name := range p.MethodNames {
		fmt.Fprintf(&out, "case %q: return t.MethodByName(%q)\n", name, name)
	}
	fmt.Fprintln(&out, "}; requireCovered(t); return reflect.Method{}, false }")
	fmt.Fprintln(&out, "func ValueByName(v reflect.Value, name string) reflect.Value { switch name {")
	for _, name := range p.MethodNames {
		fmt.Fprintf(&out, "case %q: return v.MethodByName(%q)\n", name, name)
	}
	fmt.Fprintln(&out, "}; requireCovered(v.Type()); return reflect.Value{} }")
	out.WriteString(`// Unknown host method sets must never be silently treated as missing methods.
func requireCovered(t reflect.Type) {
	if t.NumMethod() == 0 { return }
	known := 0
	for _, name := range methodNames { if _, ok := TypeByName(t, name); ok { known++ } }
	if known != t.NumMethod() { panic(fmt.Sprintf("expr_static_methods: unvalidated method set on %v; regenerate and validate profile", t)) }
}
func TypeByIndex(t reflect.Type, index int) reflect.Method {
	for _, name := range methodNames { if method, ok := TypeByName(t, name); ok && method.Index == index { return method } }
	panic(fmt.Sprintf("expr_static_methods: unvalidated method index %d on %v", index, t))
}
func ValueByIndex(v reflect.Value, index int) reflect.Value {
	return ValueByName(v, TypeByIndex(v.Type(), index).Name)
}`)
	data, err := format.Source(out.Bytes())
	if err != nil {
		return fmt.Errorf("format static methods: %w", err)
	}
	if writeErr := output(filepath.Join(candidate, "internal", "staticmethod", "methods_static.go"), data, check); writeErr != nil {
		return writeErr
	}
	manifest, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return fmt.Errorf("encode profile: %w", err)
	}
	if writeErr := output(filepath.Join(root, "scripts", "expr-method-profile", "profile.json"), manifest, check); writeErr != nil {
		return writeErr
	}
	guard := fmt.Sprintf(`//go:build expr_static_methods

// Code generated by the Core method-profile generator. DO NOT EDIT.
package staticmethod
import ("crypto/sha256"; "encoding/hex"; "fmt"; "io/fs"; "os"; "path/filepath"; "runtime"; "strings"; "testing")
func TestProfileUpgradeGate(t *testing.T) {
	if runtime.Version() != %q { t.Fatalf("Go toolchain changed: %%s; regenerate and differentially validate static method profile", runtime.Version()) }
	root := filepath.Join("..", "..")
	h := sha256.New()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil { return err }
		rel, err := filepath.Rel(root, path); if err != nil { return err }; rel = filepath.ToSlash(rel)
		if d.IsDir() {
            if rel == "internal/staticmethod" || strings.HasPrefix(d.Name(), ".") && rel != "." { return filepath.SkipDir }
            if rel != "." { if _, err := os.Stat(filepath.Join(path, "go.mod")); err == nil { return filepath.SkipDir } else if !os.IsNotExist(err) { return err } }
            return nil
        }
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") { return nil }
		data, err := os.ReadFile(path); if err != nil { return err }
		fmt.Fprintf(h, "%%s\x00", rel); h.Write(data); h.Write([]byte{0}); return nil
	})
	if err != nil { t.Fatal(err) }
	if got := hex.EncodeToString(h.Sum(nil)); got != %q { t.Fatalf("Expr source changed (%%s); regenerate and differentially validate profile", got) }
}
`, p.GoVersion, p.ExprSourceSHA256)
	guardData, err := format.Source([]byte(guard))
	if err != nil {
		return fmt.Errorf("format upgrade guard: %w", err)
	}
	if writeErr := output(filepath.Join(candidate, "internal", "staticmethod", "upgrade_static_test.go"), guardData, check); writeErr != nil {
		return writeErr
	}
	log.Info().Bool("check", check).Int("methods", len(p.MethodNames)).Str("go", p.GoVersion).Msg("static profile valid")
	return nil
}

func output(path string, data []byte, check bool) error {
	if check {
		// #nosec G304 -- Reads only selected Expr source or the checked-in profile.
		old, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read profile artifact: %w", err)
		}
		if !bytes.Equal(old, data) {
			return fmt.Errorf("profile drift: %s; review change, regenerate, and rerun differential suite", path)
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("create profile directory: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write profile artifact: %w", err)
	}
	return nil
}

func environmentSchema(t reflect.Type, path string, schema *[]string) error {
	*schema = append(*schema, path+":"+t.String())
	switch t.Kind() {
	case reflect.Invalid, reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128, reflect.String:
		// Scalar fields cannot expose additional host object types.
	case reflect.Interface, reflect.Func, reflect.Chan, reflect.UnsafePointer:
		return fmt.Errorf("unbounded expression environment type at %s: %v", path, t)
	case reflect.Pointer, reflect.Array, reflect.Slice, reflect.Map:
		return fmt.Errorf("new expression environment container at %s needs explicit audit: %v", path, t)
	case reflect.Struct:
		for i := range t.NumField() {
			f := t.Field(i)
			if !f.IsExported() {
				continue
			}
			*schema = append(*schema, path+"."+f.Name+":"+string(f.Tag))
			if err := environmentSchema(f.Type, path+"."+f.Name, schema); err != nil {
				return err
			}
		}
	}
	return nil
}

func sourceDigest(root string) (string, error) {
	source, err := os.OpenRoot(root)
	if err != nil {
		return "", fmt.Errorf("open Expr source root: %w", err)
	}
	defer func() { _ = source.Close() }()
	h := sha256.New()
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return fmt.Errorf("resolve source path: %w", err)
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel == "internal/staticmethod" || strings.HasPrefix(d.Name(), ".") && rel != "." {
				return filepath.SkipDir
			}
			// Module archives omit nested modules; Git checkouts must hash identically.
			if rel != "." {
				if _, statErr := source.Stat(filepath.Join(rel, "go.mod")); statErr == nil {
					return filepath.SkipDir
				} else if !os.IsNotExist(statErr) {
					return fmt.Errorf("inspect nested module: %w", statErr)
				}
			}
			return nil
		}
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
			return nil
		}
		data, err := source.ReadFile(rel)
		if err != nil {
			return fmt.Errorf("read Expr source: %w", err)
		}
		// hash.Hash.Write never returns an error.
		_, _ = h.Write([]byte(rel + "\x00"))
		_, _ = h.Write(data)
		_, _ = h.Write([]byte{0})
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("hash Expr source: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
