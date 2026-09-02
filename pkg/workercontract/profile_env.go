package workercontract

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// ProfileFileEnv는 stack worker profile 파일 경로를 담는 환경 변수다.
const ProfileFileEnv = "STACK_WORKER_PROFILE_FILE"

// LoadProfileFromEnv는 ProfileFileEnv가 가리키는 profile 파일을 service/role identity로 검증해 읽는다.
// 값이 없거나 앞뒤 공백이 있으면 거부한다.
func LoadProfileFromEnv(service, role string) (LoadedProfile, error) {
	path, present := os.LookupEnv(ProfileFileEnv)
	if !present || path == "" {
		return LoadedProfile{}, errors.New(ProfileFileEnv + " is required")
	}

	if path != strings.TrimSpace(path) {
		return LoadedProfile{}, errors.New(ProfileFileEnv + " must not contain surrounding whitespace")
	}

	identity, err := KnownIdentity(service, role)
	if err != nil {
		return LoadedProfile{}, fmt.Errorf("resolve %s/%s worker identity: %w", service, role, err)
	}

	loaded, err := LoadProfileFile(path, identity)
	if err != nil {
		return LoadedProfile{}, fmt.Errorf("load profile file: %w", err)
	}

	return loaded, nil
}
