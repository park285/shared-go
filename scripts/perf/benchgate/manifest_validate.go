package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

func strictJSON(data []byte, out any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := checkJSONValue(decoder); err != nil {
		return err
	}
	if decoder.More() {
		return perr("manifest contains trailing JSON values")
	}
	decoder = json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return perr("invalid manifest: %v", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return perr("manifest contains trailing JSON data")
	}
	var role string
	switch out.(type) {
	case *CandidateManifest:
		role = "candidate"
	case *BaselineManifest:
		role = "baseline"
	}
	if role != "" {
		if err := requireManifestFields(data, role); err != nil {
			return err
		}
	}
	return nil
}

func requireFields(values map[string]json.RawMessage, path string, fields []string) error {
	for _, field := range fields {
		if _, ok := values[field]; !ok {
			return perr("manifest missing required field: %s.%s", path, field)
		}
	}
	return nil
}

func requireObject(raw json.RawMessage, path string, fields []string) (map[string]json.RawMessage, error) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		return nil, perr("manifest field must be an object: %s", path)
	}
	if err := requireFields(object, path, fields); err != nil {
		return nil, err
	}
	return object, nil
}

func requireManifestFields(data []byte, role string) error {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil || root == nil {
		return perr("manifest root must be an object")
	}
	fields := []string{"schema_version", "evidence_role", "git_sha", "git_dirty", "created_at", "identity", "environment", "collection", "files"}
	if role == "baseline" {
		fields = append(fields, "approved_sha")
	}
	if err := requireFields(root, "manifest", fields); err != nil {
		return err
	}
	if _, err := requireObject(root["identity"], "identity", []string{"contract", "repository", "gate_id", "selection_gate", "policy_sha256", "selection_sha256", "harness_sha256", "benchmarks"}); err != nil {
		return err
	}
	if _, err := requireObject(root["environment"], "environment", []string{"go_version", "goos", "goarch", "cpu_model"}); err != nil {
		return err
	}
	collection, err := requireObject(root["collection"], "collection", []string{"count", "benchtime", "benchmem", "race", "commands"})
	if err != nil {
		return err
	}
	var commands []json.RawMessage
	if err := json.Unmarshal(collection["commands"], &commands); err != nil {
		return perr("manifest collection.commands must be an array")
	}
	for index, commandRaw := range commands {
		if _, err := requireObject(commandRaw, fmt.Sprintf("collection.commands[%d]", index), []string{"package", "workdir", "argv"}); err != nil {
			return err
		}
	}
	return nil
}

func checkJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return perr("invalid manifest JSON: %v", err)
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for decoder.More() {
			key, err := decoder.Token()
			if err != nil {
				return perr("invalid manifest JSON: %v", err)
			}
			name, ok := key.(string)
			if !ok {
				return perr("invalid manifest object key")
			}
			if seen[name] {
				return perr("manifest contains duplicate key: %s", name)
			}
			seen[name] = true
			if err := checkJSONValue(decoder); err != nil {
				return err
			}
		}
		if _, err := decoder.Token(); err != nil {
			return perr("invalid manifest JSON: %v", err)
		}
	case '[':
		for decoder.More() {
			if err := checkJSONValue(decoder); err != nil {
				return err
			}
		}
		if _, err := decoder.Token(); err != nil {
			return perr("invalid manifest JSON: %v", err)
		}
	default:
		return perr("invalid manifest JSON delimiter")
	}
	return nil
}
