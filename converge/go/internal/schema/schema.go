// Package schema validates converge critique payloads against the embedded
// JSON Schema. It uses a minimal hand-rolled validator (stdlib only) covering
// the constraints we care about: types, required fields, enums, min/max,
// pattern, additionalProperties=false, and array items.
package schema

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Validate returns a list of error messages (empty if valid). When
// requireEvidence is true, every issue must additionally have non-empty
// `file` and `line_start` / `line_end` (used for implement/verify/review).
func Validate(payload, schema []byte, requireEvidence bool) ([]string, error) {
	var doc, sch any
	if err := json.Unmarshal(payload, &doc); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	if err := json.Unmarshal(schema, &sch); err != nil {
		return nil, fmt.Errorf("invalid schema JSON: %w", err)
	}
	v := &validator{}
	v.walk("$", doc, sch)

	if requireEvidence {
		root, _ := doc.(map[string]any)
		issues, _ := root["issues"].([]any)
		for i, it := range issues {
			obj, _ := it.(map[string]any)
			ctx := fmt.Sprintf("$.issues[%d]", i)
			f, _ := obj["file"].(string)
			if strings.TrimSpace(f) == "" {
				v.errs = append(v.errs, fmt.Sprintf("%s: 'file' required for this mode", ctx))
			}
			for _, k := range []string{"line_start", "line_end"} {
				val, ok := obj[k]
				if !ok {
					v.errs = append(v.errs, fmt.Sprintf("%s: '%s' required for this mode", ctx, k))
					continue
				}
				if n, ok := val.(float64); !ok || n < 1 {
					v.errs = append(v.errs, fmt.Sprintf("%s: '%s' must be int >= 1", ctx, k))
				}
			}
		}
	}

	sort.Strings(v.errs)
	return v.errs, nil
}

type validator struct {
	errs []string
}

func (v *validator) addf(path, format string, args ...any) {
	v.errs = append(v.errs, path+": "+fmt.Sprintf(format, args...))
}

func (v *validator) walk(path string, doc, sch any) {
	s, ok := sch.(map[string]any)
	if !ok {
		return
	}

	if t, ok := s["type"].(string); ok && !typeMatches(t, doc) {
		v.addf(path, "wrong type, want %s", t)
		return
	}

	switch d := doc.(type) {
	case map[string]any:
		v.walkObject(path, d, s)
	case []any:
		v.walkArray(path, d, s)
	case string:
		v.walkString(path, d, s)
	case float64:
		v.walkNumber(path, d, s)
	}

	if enum, ok := s["enum"].([]any); ok {
		match := false
		for _, e := range enum {
			if fmt.Sprintf("%v", e) == fmt.Sprintf("%v", doc) {
				match = true
				break
			}
		}
		if !match {
			vals := make([]string, len(enum))
			for i, e := range enum {
				vals[i] = fmt.Sprintf("%v", e)
			}
			v.addf(path, "must be one of [%s], got %v", strings.Join(vals, ", "), doc)
		}
	}
}

func (v *validator) walkObject(path string, d map[string]any, s map[string]any) {
	if req, ok := s["required"].([]any); ok {
		for _, r := range req {
			k, _ := r.(string)
			if _, present := d[k]; !present {
				v.addf(path, "missing required field %q", k)
			}
		}
	}
	addProps, addOK := s["additionalProperties"]
	props, _ := s["properties"].(map[string]any)
	for k, val := range d {
		sub, ok := props[k].(map[string]any)
		if !ok {
			if addOK {
				if b, isBool := addProps.(bool); isBool && !b {
					v.addf(path, "unknown field %q", k)
					continue
				}
			}
			continue
		}
		v.walk(path+"."+k, val, sub)
	}
}

func (v *validator) walkArray(path string, d []any, s map[string]any) {
	if max, ok := s["maxItems"].(float64); ok && len(d) > int(max) {
		v.addf(path, "max %d items, got %d", int(max), len(d))
	}
	if min, ok := s["minItems"].(float64); ok && len(d) < int(min) {
		v.addf(path, "min %d items, got %d", int(min), len(d))
	}
	if items, ok := s["items"].(map[string]any); ok {
		for i, e := range d {
			v.walk(fmt.Sprintf("%s[%d]", path, i), e, items)
		}
	}
}

func (v *validator) walkString(path, d string, s map[string]any) {
	if min, ok := s["minLength"].(float64); ok && len(d) < int(min) {
		v.addf(path, "minLength %d", int(min))
	}
	if pat, ok := s["pattern"].(string); ok {
		re, err := regexp.Compile(pat)
		if err == nil && !re.MatchString(d) {
			v.addf(path, "does not match pattern %q", pat)
		}
	}
}

func (v *validator) walkNumber(path string, d float64, s map[string]any) {
	if min, ok := s["minimum"].(float64); ok && d < min {
		v.addf(path, "minimum %v", min)
	}
	if max, ok := s["maximum"].(float64); ok && d > max {
		v.addf(path, "maximum %v", max)
	}
}

func typeMatches(t string, d any) bool {
	switch t {
	case "object":
		_, ok := d.(map[string]any)
		return ok
	case "array":
		_, ok := d.([]any)
		return ok
	case "string":
		_, ok := d.(string)
		return ok
	case "integer":
		n, ok := d.(float64)
		return ok && n == float64(int64(n))
	case "number":
		_, ok := d.(float64)
		return ok
	case "boolean":
		_, ok := d.(bool)
		return ok
	case "null":
		return d == nil
	}
	return true
}
