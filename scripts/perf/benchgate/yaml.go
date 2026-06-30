package main

import (
	"os"
	"regexp"
	"strconv"

	"gopkg.in/yaml.v3"
)

var (
	scalarIntRe   = regexp.MustCompile(`^-?\d+$`)
	scalarFloatRe = regexp.MustCompile(`^-?\d+\.\d+$`)
)

type omap struct {
	keys []string
	vals map[string]any
}

func (m *omap) get(k string) (any, bool) {
	v, ok := m.vals[k]
	return v, ok
}

func loadPolicy(path string) (*omap, *policyError) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, perr("cannot read policy file %s: %v", path, err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, perr("%v", err)
	}
	v, cerr := nodeToValue(&doc)
	if cerr != nil {
		return nil, cerr
	}
	if v == nil {
		return &omap{vals: map[string]any{}}, nil
	}
	m, ok := v.(*omap)
	if !ok {
		return nil, perr("policy must be a mapping")
	}
	return m, nil
}

func nodeToValue(n *yaml.Node) (any, *policyError) {
	switch n.Kind {
	case yaml.DocumentNode:
		if len(n.Content) == 0 {
			return nil, nil
		}
		return nodeToValue(n.Content[0])
	case yaml.MappingNode:
		m := &omap{vals: map[string]any{}}
		for i := 0; i+1 < len(n.Content); i += 2 {
			key := n.Content[i].Value
			val, err := nodeToValue(n.Content[i+1])
			if err != nil {
				return nil, err
			}
			if _, exists := m.vals[key]; !exists {
				m.keys = append(m.keys, key)
			}
			m.vals[key] = val
		}
		return m, nil
	case yaml.SequenceNode:
		arr := make([]any, 0, len(n.Content))
		for _, c := range n.Content {
			v, err := nodeToValue(c)
			if err != nil {
				return nil, err
			}
			arr = append(arr, v)
		}
		return arr, nil
	case yaml.AliasNode:
		return nodeToValue(n.Alias)
	case yaml.ScalarNode:
		return parseScalar(n), nil
	}
	return nil, nil
}

func parseScalar(n *yaml.Node) any {
	if n.Style&yaml.DoubleQuotedStyle != 0 || n.Style&yaml.SingleQuotedStyle != 0 {
		return n.Value
	}
	switch n.Value {
	case "true", "True":
		return true
	case "false", "False":
		return false
	}
	if scalarIntRe.MatchString(n.Value) {
		if i, err := strconv.Atoi(n.Value); err == nil {
			return i
		}
	}
	if scalarFloatRe.MatchString(n.Value) {
		if f, err := strconv.ParseFloat(n.Value, 64); err == nil {
			return f
		}
	}
	return n.Value
}
