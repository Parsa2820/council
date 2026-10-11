package harness

import (
	"strconv"
	"strings"
)

// Lookup walks a decoded JSON document with a dot-separated path. Numeric
// segments index arrays; negative indices count from the end. An empty path
// returns the whole document.
func Lookup(doc any, path string) (any, bool) {
	path = strings.TrimPrefix(strings.TrimSpace(path), ".")
	if path == "" {
		return doc, true
	}
	cur := doc
	for _, seg := range strings.Split(path, ".") {
		switch node := cur.(type) {
		case map[string]any:
			v, ok := node[seg]
			if !ok {
				return nil, false
			}
			cur = v
		case []any:
			i, err := strconv.Atoi(seg)
			if err != nil {
				return nil, false
			}
			if i < 0 {
				i += len(node)
			}
			if i < 0 || i >= len(node) {
				return nil, false
			}
			cur = node[i]
		default:
			return nil, false
		}
	}
	return cur, true
}
