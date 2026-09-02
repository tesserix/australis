package main

import (
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const module = "github.com/tesserix/australis"

func main() {
	var violations []string
	err := filepath.WalkDir("internal", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(path) != ".go" {
			return nil
		}

		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		for _, imported := range file.Imports {
			name, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				return fmt.Errorf("parse import in %s: %w", path, err)
			}
			if violation := forbiddenImport(filepath.ToSlash(path), name); violation != "" {
				violations = append(violations, violation)
			}
		}
		return nil
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if len(violations) == 0 {
		return
	}
	for _, violation := range violations {
		fmt.Fprintln(os.Stderr, violation)
	}
	os.Exit(1)
}

func forbiddenImport(path, imported string) string {
	if strings.HasPrefix(imported, module+"/servers/") {
		return fmt.Sprintf("%s imports connector fleet package %s", path, imported)
	}
	if strings.HasPrefix(path, "internal/core/") {
		if strings.HasPrefix(imported, module+"/internal/adapter/") {
			return fmt.Sprintf("%s imports adapter package %s", path, imported)
		}
		if strings.Contains(imported, "modelcontextprotocol") {
			return fmt.Sprintf("%s imports MCP SDK %s", path, imported)
		}
	}
	if strings.Contains(imported, "modelcontextprotocol") && !strings.HasPrefix(path, "internal/adapter/mcp/") {
		return fmt.Sprintf("%s imports MCP SDK outside internal/adapter/mcp: %s", path, imported)
	}
	return ""
}
