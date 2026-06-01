package mapping

import (
	"fmt"
	"strings"

	"github.com/jmespath/go-jmespath"
)

func Apply(source map[string]interface{}, fields map[string]string) (map[string]interface{}, error) {
	out := make(map[string]interface{}, len(fields))
	for name, expression := range fields {
		value, err := Search(source, expression)
		if err != nil {
			return nil, fmt.Errorf("map %q from %q: %w", name, expression, err)
		}
		out[name] = value
	}
	return out, nil
}

func Search(source map[string]interface{}, expression string) (interface{}, error) {
	return jmespath.Search(normalizeExpression(expression), source)
}

func MergeAtPath(target map[string]interface{}, path string, values map[string]interface{}) error {
	if path == "" || path == "$" {
		for key, value := range values {
			target[key] = value
		}
		return nil
	}

	parts := strings.Split(normalizeExpression(path), ".")
	if len(parts) == 0 {
		return nil
	}
	if parts[0] == "context" {
		parts = parts[1:]
	}
	if len(parts) == 0 {
		for key, value := range values {
			target[key] = value
		}
		return nil
	}

	current := target
	for _, part := range parts[:len(parts)-1] {
		next, ok := current[part].(map[string]interface{})
		if !ok {
			next = map[string]interface{}{}
			current[part] = next
		}
		current = next
	}
	leaf := parts[len(parts)-1]
	existing, ok := current[leaf].(map[string]interface{})
	if !ok {
		existing = map[string]interface{}{}
		current[leaf] = existing
	}
	for key, value := range values {
		existing[key] = value
	}
	return nil
}

func normalizeExpression(expression string) string {
	expression = strings.TrimSpace(expression)
	expression = strings.TrimPrefix(expression, "$.")
	expression = strings.TrimPrefix(expression, "$")
	return expression
}
