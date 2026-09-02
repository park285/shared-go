package workercontract

import (
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
)

// DecodeWorkerSettings는 workerID의 service-owned settings를 destination(struct pointer)에 해석한다.
//
// Settings의 키 집합은 destination의 json 태그 필드 집합과 정확히 같아야 한다. 알 수 없는 키는
// DecodeSettings가, 빠진 키는 이 함수가 거부하므로 zero value로 조용히 채워지는 설정이 없다.
func DecodeWorkerSettings(loaded LoadedProfile, workerID string, destination any) error {
	worker, ok := loaded.Profile.Workers[workerID]
	if !ok {
		return fmt.Errorf("decode %s settings: worker is missing", workerID)
	}

	expected, err := settingsKeys(destination)
	if err != nil {
		return fmt.Errorf("decode %s settings: %w", workerID, err)
	}

	if err := DecodeSettings(worker.Settings, destination); err != nil {
		return fmt.Errorf("decode %s settings: %w", workerID, err)
	}

	var fields map[string]jsontext.Value

	if err := jsonv2.Unmarshal(worker.Settings, &fields); err != nil {
		return fmt.Errorf("decode %s settings keys: %w", workerID, err)
	}

	actual := slices.Sorted(maps.Keys(fields))
	if !slices.Equal(actual, expected) {
		return fmt.Errorf("decode %s settings: got keys %v, want %v", workerID, actual, expected)
	}

	return nil
}

func settingsKeys(destination any) ([]string, error) {
	typ := reflect.TypeOf(destination)
	if typ == nil || typ.Kind() != reflect.Pointer || typ.Elem().Kind() != reflect.Struct {
		return nil, errors.New("settings destination must be a struct pointer")
	}

	typ = typ.Elem()

	keys := make([]string, 0, typ.NumField())

	for field := range typ.Fields() {
		if field.Anonymous {
			return nil, errors.New("settings destination must not embed structs")
		}

		if !field.IsExported() {
			continue
		}

		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")

		switch name {
		case "-":
			continue
		case "":
			name = field.Name
		}

		keys = append(keys, name)
	}

	slices.Sort(keys)

	return keys, nil
}
