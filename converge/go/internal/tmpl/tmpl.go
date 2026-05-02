// Package tmpl interpolates {{PLACEHOLDER}} tokens in prompt templates.
// Supports {{IF_RESUME}}...{{ENDIF_RESUME}} blocks toggled by a "RESUME"
// value of "1"/"true"/"yes".
package tmpl

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

var (
	resumeBlock = regexp.MustCompile(`(?s)\{\{IF_RESUME\}\}(.*?)\{\{ENDIF_RESUME\}\}`)
	placeholder = regexp.MustCompile(`\{\{([A-Z_][A-Z0-9_]*)\}\}`)
)

// LoadValue interprets a "KEY=value" or "KEY=@/path" pair.
type Value struct {
	Key string
	Val string
}

// Parse splits "KEY=…" forms and reads files for the @prefix variant.
func Parse(pairs []string) ([]Value, error) {
	out := make([]Value, 0, len(pairs))
	for _, p := range pairs {
		i := strings.Index(p, "=")
		if i < 0 {
			return nil, fmt.Errorf("bad arg (no =): %s", p)
		}
		k, v := p[:i], p[i+1:]
		if strings.HasPrefix(v, "@") {
			b, err := os.ReadFile(v[1:])
			if err != nil {
				return nil, fmt.Errorf("cannot read %s: %w", v[1:], err)
			}
			v = string(b)
		}
		out = append(out, Value{Key: k, Val: v})
	}
	return out, nil
}

// Render interpolates placeholders in `text`. Returns the rendered string and
// a sorted list of placeholders that were referenced but not provided.
func Render(text string, vals []Value) (string, []string) {
	values := make(map[string]string, len(vals))
	resume := false
	for _, v := range vals {
		values[v.Key] = v.Val
		if v.Key == "RESUME" {
			t := strings.TrimSpace(strings.ToLower(v.Val))
			if t == "1" || t == "true" || t == "yes" {
				resume = true
			}
		}
	}

	text = resumeBlock.ReplaceAllStringFunc(text, func(m string) string {
		sub := resumeBlock.FindStringSubmatch(m)
		if resume {
			return sub[1]
		}
		return ""
	})

	missing := map[string]struct{}{}
	out := placeholder.ReplaceAllStringFunc(text, func(m string) string {
		k := placeholder.FindStringSubmatch(m)[1]
		if v, ok := values[k]; ok {
			return v
		}
		missing[k] = struct{}{}
		return ""
	})

	keys := make([]string, 0, len(missing))
	for k := range missing {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return out, keys
}
