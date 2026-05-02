// Package embedded ships the schema and prompts inside the binary so it's
// self-contained, with optional filesystem overrides for local iteration.
package embedded

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Source layout (rebuilt from <repo>/converge/{schemas,prompts}/):
//
//   converge/schemas/critique.schema.json   ->  schemas/critique.schema.json
//   converge/prompts/*.tmpl                 ->  prompts/*.tmpl
//   converge/prompts/blocks.md              ->  prompts/blocks.md

//go:embed schemas/critique.schema.json prompts/*.tmpl prompts/blocks.md
var fsys embed.FS

// SchemaBytes returns the critique JSON Schema. If $CONVERGE_SCHEMA is set,
// it is read from the filesystem first.
func SchemaBytes() ([]byte, error) {
	if p := os.Getenv("CONVERGE_SCHEMA"); p != "" {
		return os.ReadFile(p)
	}
	return fs.ReadFile(fsys, "schemas/critique.schema.json")
}

// TemplateBytes returns the named prompt template ("plan", "implement",
// "verify", or "review"). If $CONVERGE_PROMPTS_DIR is set, it overrides the
// embedded copy.
func TemplateBytes(mode string) ([]byte, error) {
	name := mode + ".tmpl"
	if d := os.Getenv("CONVERGE_PROMPTS_DIR"); d != "" {
		return os.ReadFile(filepath.Join(d, name))
	}
	b, err := fs.ReadFile(fsys, "prompts/"+name)
	if err != nil {
		return nil, fmt.Errorf("unknown mode %q: %w", mode, err)
	}
	return b, nil
}

// ListEmbeddedTemplates returns the modes that have embedded templates.
func ListEmbeddedTemplates() []string {
	entries, _ := fs.ReadDir(fsys, "prompts")
	var modes []string
	for _, e := range entries {
		n := e.Name()
		if strings.HasSuffix(n, ".tmpl") {
			modes = append(modes, strings.TrimSuffix(n, ".tmpl"))
		}
	}
	return modes
}
