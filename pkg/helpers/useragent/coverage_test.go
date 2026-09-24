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

package useragent

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const useragentImport = "github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/useragent"

// exempt lists files whose HTTP use cannot or need not go through this
// package.
var exempt = map[string]bool{
	// Installs custom TLS roots into http.DefaultClient; sends nothing.
	filepath.Join("pkg", "helpers", "tlsroots", "tlsroots.go"): true,
	// sentry-go always overwrites the User-Agent with its own SDK name.
	filepath.Join("internal", "telemetry", "telemetry.go"): true,
}

// TestEveryHTTPClientSendsUserAgent fails when production code can send an
// HTTP request that bypasses the User-Agent: an http.Client whose Transport is
// not wrapped by Transport, the package-level http request helpers or
// http.DefaultClient, or a WebSocket dial without Header.
func TestEveryHTTPClientSendsUserAgent(t *testing.T) {
	t.Parallel()

	root := filepath.Join("..", "..", "..")
	var offenders []string
	for _, dir := range []string{"cmd", "internal", "pkg"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				// pkg/testing is test infrastructure that never ships.
				if d.Name() == "testdata" || path == filepath.Join(root, "pkg", "testing") {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err //nolint:wrapcheck // Test walk.
			}
			if !exempt[rel] {
				offenders = append(offenders, checkFile(t, path, rel)...)
			}
			return nil
		})
		require.NoError(t, err)
	}
	assert.Empty(t, offenders, "outbound HTTP must send the Zaparoo User-Agent")
}

func checkFile(t *testing.T, path, rel string) []string {
	t.Helper()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	require.NoError(t, err, path)

	httpName, uaName := "", ""
	for _, imp := range file.Imports {
		importPath, err := strconv.Unquote(imp.Path.Value)
		require.NoError(t, err)
		name := importPath[strings.LastIndex(importPath, "/")+1:]
		if imp.Name != nil {
			name = imp.Name.Name
		}
		switch importPath {
		case "net/http":
			httpName = name
		case useragentImport:
			uaName = name
		}
	}
	if httpName == "" {
		return nil
	}

	isHTTP := func(expr ast.Expr, sel string) bool {
		s, ok := expr.(*ast.SelectorExpr)
		if !ok {
			return false
		}
		id, ok := s.X.(*ast.Ident)
		return ok && id.Name == httpName && s.Sel.Name == sel
	}
	isUATransport := func(expr ast.Expr) bool {
		call, ok := expr.(*ast.CallExpr)
		if !ok || uaName == "" {
			return false
		}
		s, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return false
		}
		id, ok := s.X.(*ast.Ident)
		return ok && id.Name == uaName && s.Sel.Name == "Transport"
	}

	var offenders []string
	report := func(n ast.Node, msg string) {
		offenders = append(offenders, rel+":"+strconv.Itoa(fset.Position(n.Pos()).Line)+": "+msg)
	}

	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.CompositeLit:
			if !isHTTP(node.Type, "Client") {
				return true
			}
			wrapped := false
			for _, elt := range node.Elts {
				kv, ok := elt.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				if key, ok := kv.Key.(*ast.Ident); ok && key.Name == "Transport" {
					wrapped = isUATransport(kv.Value)
				}
			}
			if !wrapped {
				report(node, "http.Client Transport must be useragent.Transport(...)")
			}
		case *ast.SelectorExpr:
			for _, sel := range []string{"Get", "Head", "Post", "PostForm"} {
				if isHTTP(node, sel) {
					report(node, "use a client built with useragent.Transport instead of http."+sel)
				}
			}
			if isHTTP(node, "DefaultClient") {
				report(node, "use a client built with useragent.Transport instead of http.DefaultClient")
			}
		case *ast.CallExpr:
			s, ok := node.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			// gorilla/websocket: Dial(url, header), DialContext(ctx, url, header).
			var header ast.Expr
			switch {
			case s.Sel.Name == "Dial" && len(node.Args) == 2:
				header = node.Args[1]
			case s.Sel.Name == "DialContext" && len(node.Args) == 3:
				header = node.Args[2]
			default:
				return true
			}
			if id, ok := header.(*ast.Ident); ok && id.Name == "nil" {
				report(node, "WebSocket dials must pass useragent.Header()")
			}
		}
		return true
	})
	return offenders
}

func TestCheckFileFindsBypasses(t *testing.T) {
	t.Parallel()

	src := `package x

import (
	"net/http"

	ua "github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/useragent"
	"github.com/gorilla/websocket"
)

var ok = &http.Client{Transport: ua.Transport(nil)}
var bare = &http.Client{}
var raw = &http.Client{Transport: http.DefaultTransport}

func f(d *websocket.Dialer) {
	_, _ = http.Get("http://example.invalid")
	_, _ = http.DefaultClient.Do(nil)
	_, _, _ = d.DialContext(nil, "ws://example.invalid", nil)
	_, _, _ = d.DialContext(nil, "ws://example.invalid", ua.Header())
	_, _, _ = websocket.DefaultDialer.Dial("ws://example.invalid", nil)
	_, _, _ = d.Dial("ws://example.invalid", ua.Header())
}
`
	path := filepath.Join(t.TempDir(), "x.go")
	require.NoError(t, os.WriteFile(path, []byte(src), 0o600))

	offenders := checkFile(t, path, "x.go")
	assert.Equal(t, []string{
		"x.go:11: http.Client Transport must be useragent.Transport(...)",
		"x.go:12: http.Client Transport must be useragent.Transport(...)",
		"x.go:15: use a client built with useragent.Transport instead of http.Get",
		"x.go:16: use a client built with useragent.Transport instead of http.DefaultClient",
		"x.go:17: WebSocket dials must pass useragent.Header()",
		"x.go:19: WebSocket dials must pass useragent.Header()",
	}, offenders)
}
