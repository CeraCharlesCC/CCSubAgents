package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

func readJSONFile(path string) (map[string]any, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]any{}, nil
		}
		return nil, err
	}
	if len(bytes.TrimSpace(b)) == 0 {
		return map[string]any{}, nil
	}
	normalized, err := normalizeJSONC(b)
	if err != nil {
		return nil, err
	}
	var root map[string]any
	if err := json.Unmarshal(normalized, &root); err != nil {
		return nil, err
	}
	if root == nil {
		root = map[string]any{}
	}
	return root, nil
}

func writeJSONFile(path string, v map[string]any, perm os.FileMode) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, perm)
}

func ensureObject(root map[string]any, key string) (map[string]any, error) {
	v, exists := root[key]
	if !exists {
		next := map[string]any{}
		root[key] = next
		return next, nil
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s must be an object when present", key)
	}
	return obj, nil
}

func normalizeJSONC(input []byte) ([]byte, error) {
	b := bytes.TrimPrefix(input, []byte{0xEF, 0xBB, 0xBF})
	out := make([]byte, 0, len(b))

	inString := false
	inLineComment := false
	inBlockComment := false
	escaped := false

	for i := 0; i < len(b); i++ {
		ch := b[i]

		if inLineComment {
			if ch == '\n' || ch == '\r' {
				inLineComment = false
				out = append(out, ch)
			}
			continue
		}

		if inBlockComment {
			if ch == '*' && i+1 < len(b) && b[i+1] == '/' {
				inBlockComment = false
				i++
			}
			continue
		}

		if inString {
			out = append(out, ch)
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}

		switch ch {
		case '"':
			inString = true
			out = append(out, ch)
		case '/':
			if i+1 < len(b) {
				if b[i+1] == '/' {
					inLineComment = true
					i++
					continue
				}
				if b[i+1] == '*' {
					inBlockComment = true
					i++
					continue
				}
			}
			out = append(out, ch)
		case ',':
			if hasTrailingComma(b, i+1) {
				continue
			}
			out = append(out, ch)
		default:
			out = append(out, ch)
		}
	}

	if inString {
		return nil, errors.New("unterminated JSON string")
	}
	if inBlockComment {
		return nil, errors.New("unterminated JSON block comment")
	}

	return out, nil
}

func hasTrailingComma(b []byte, start int) bool {
	for i := start; i < len(b); i++ {
		switch b[i] {
		case ' ', '\t', '\n', '\r':
			continue
		case '/':
			if i+1 >= len(b) {
				return false
			}
			if b[i+1] == '/' {
				i += 2
				for i < len(b) && b[i] != '\n' && b[i] != '\r' {
					i++
				}
				i--
				continue
			}
			if b[i+1] == '*' {
				i += 2
				for i+1 < len(b) && !(b[i] == '*' && b[i+1] == '/') {
					i++
				}
				if i+1 >= len(b) {
					return false
				}
				i++
				continue
			}
			return false
		case '}', ']':
			return true
		default:
			return false
		}
	}
	return false
}
