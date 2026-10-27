package workercontract

import (
	"bytes"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
)

// DecodeWorkerSettingsSet는 모든 worker의 decoder를 빠짐없이 지정한 뒤 strict decode한다.
// 이 함수가 settings를 읽는 유일한 공개 경로이며, 각 worker settings는 decodeWorkerSettings 규칙(정확한 키 집합, 포인터 외 null 거부)을 따른다.
func DecodeWorkerSettingsSet(loaded LoadedProfile, destinations map[string]any) error {
	if len(loaded.Profile.Workers) != len(destinations) {
		return errors.New("workercontract: worker settings: decoder worker set must match profile")
	}

	ids := sortedWorkerIDs(loaded.Profile.Workers)
	for _, id := range ids {
		if _, ok := destinations[id]; !ok {
			return fmt.Errorf("workercontract: decode %s settings: worker decoder is missing", id)
		}
	}

	for _, id := range ids {
		if err := decodeWorkerSettings(loaded, id, destinations[id]); err != nil {
			return err
		}
	}

	return nil
}

// decodeWorkerSettings는 workerID의 service-owned settings를 destination(struct pointer)에 해석한다.
//
// Settings의 모든 struct 키 집합은 json 태그 필드 집합과 정확히 같아야 한다.
// 포인터 외의 null과 빠진 키는 거부하므로 zero value로 조용히 채워지는 설정이 없다.
func decodeWorkerSettings(loaded LoadedProfile, workerID string, destination any) error {
	worker, ok := loaded.Profile.Workers[workerID]
	if !ok {
		return fmt.Errorf("workercontract: decode %s settings: worker is missing", workerID)
	}

	typ := reflect.TypeOf(destination)
	if typ == nil || typ.Kind() != reflect.Pointer || typ.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("workercontract: decode %s settings: settings destination must be a struct pointer", workerID)
	}

	if reflect.ValueOf(destination).IsNil() {
		return fmt.Errorf("workercontract: decode %s settings: settings destination must not be nil", workerID)
	}

	if err := decodeSettings(worker.Settings, destination); err != nil {
		return fmt.Errorf("workercontract: decode %s settings: %w", workerID, err)
	}

	if err := validateSettingsValue(worker.Settings, typ.Elem(), "settings"); err != nil {
		return fmt.Errorf("workercontract: decode %s settings: %w", workerID, err)
	}

	return nil
}

func validateSettingsValue(raw jsontext.Value, typ reflect.Type, path string) error {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		if typ.Kind() != reflect.Pointer {
			return fmt.Errorf("%s: null requires a pointer field", path)
		}

		return nil
	}

	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}

	switch {
	case typ.Kind() == reflect.Struct:
		return validateSettingsStruct(raw, typ, path)
	case typ.Kind() == reflect.Map:
		return validateSettingsMap(raw, typ, path)
	case typ.Kind() == reflect.Slice || typ.Kind() == reflect.Array:
		return validateSettingsArray(raw, typ, path)
	default:
		return nil
	}
}

func validateSettingsStruct(raw jsontext.Value, typ reflect.Type, path string) error {
	expected, err := settingsKeys(typ)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}

	var fields map[string]jsontext.Value

	if err := jsonv2.Unmarshal(raw, &fields); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}

	actual := slices.Sorted(maps.Keys(fields))
	if !slices.Equal(actual, expected) {
		return fmt.Errorf("%s: got keys %v, want %v", path, actual, expected)
	}

	for field := range typ.Fields() {
		name, included := settingsFieldName(field)
		if !included {
			continue
		}

		if err := validateSettingsValue(fields[name], field.Type, path+"."+name); err != nil {
			return err
		}
	}

	return nil
}

func validateSettingsMap(raw jsontext.Value, typ reflect.Type, path string) error {
	var entries map[string]jsontext.Value

	if err := jsonv2.Unmarshal(raw, &entries); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}

	for key, value := range entries {
		if err := validateSettingsValue(value, typ.Elem(), fmt.Sprintf("%s[%q]", path, key)); err != nil {
			return err
		}
	}

	return nil
}

func validateSettingsArray(raw jsontext.Value, typ reflect.Type, path string) error {
	// 바이트 슬라이스는 JSON 배열 대신 문자열로 인코딩될 수 있다.
	if bytes.TrimSpace(raw)[0] != '[' {
		return nil
	}

	var elements []jsontext.Value

	if err := jsonv2.Unmarshal(raw, &elements); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}

	for index, value := range elements {
		if err := validateSettingsValue(value, typ.Elem(), fmt.Sprintf("%s[%d]", path, index)); err != nil {
			return err
		}
	}

	return nil
}

func settingsKeys(typ reflect.Type) ([]string, error) {
	keys := make([]string, 0, typ.NumField())

	for field := range typ.Fields() {
		if field.Anonymous {
			return nil, errors.New("settings destination must not embed structs")
		}

		if name, included := settingsFieldName(field); included {
			keys = append(keys, name)
		}
	}

	slices.Sort(keys)

	return keys, nil
}

func settingsFieldName(field reflect.StructField) (string, bool) {
	if !field.IsExported() {
		return "", false
	}

	name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
	if name == "-" {
		return "", false
	}

	if name == "" {
		name = field.Name
	}

	return name, true
}
