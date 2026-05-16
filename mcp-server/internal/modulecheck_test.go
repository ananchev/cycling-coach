package internal_test

import (
	"bytes"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestModuleBoundary_NoCyclingCoachImport walks every .go file under the
// mcp-server module root and asserts none import a cycling-coach/* package.
// This guards the locked decision (§2 of MCP_SERVER_SPEC.md): the MCP server
// shares no code with the app; the only coupling is the HTTP+JSON contract.
func TestModuleBoundary_NoCyclingCoachImport(t *testing.T) {
	root := ".." // mcp-server/ directory from mcp-server/internal/
	fset := token.NewFileSet()

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case "contract", "vendor", ".git":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			// Non-fatal: the file may simply be unparseable; build will catch real errors.
			return nil
		}
		for _, imp := range f.Imports {
			importPath, _ := strconv.Unquote(imp.Path.Value)
			if strings.HasPrefix(importPath, "cycling-coach/") {
				t.Errorf("%s imports cycling-coach package %q — module boundary violated", path, importPath)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk mcp-server/: %v", err)
	}
}

// TestModuleBoundary_GoModHasNoCyclingCoachDep asserts the mcp-server go.mod
// does not list cycling-coach as a direct or indirect dependency.
func TestModuleBoundary_GoModHasNoCyclingCoachDep(t *testing.T) {
	data, err := os.ReadFile("../go.mod")
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	if bytes.Contains(data, []byte("cycling-coach")) {
		t.Error("go.mod requires cycling-coach — module boundary violated")
	}
}
