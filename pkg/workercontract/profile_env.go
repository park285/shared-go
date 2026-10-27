package workercontract

import (
	"errors"
	"os"
	"strings"
)

// ProfileFileEnv는 stack worker profile 파일 경로를 담는 환경 변수다.
const ProfileFileEnv = "STACK_WORKER_PROFILE_FILE"

// ProfileSourceFileEnv는 release profile과 비교할 원하는 원본 경로다. 설정을 읽는 경로가 아니다.
const ProfileSourceFileEnv = "STACK_WORKER_PROFILE_SOURCE_FILE"

// LoadProfileFromEnv는 ProfileFileEnv가 가리키는 profile 파일을 호출 서비스가 소유한 identity로 검증해 읽는다.
// 값이 없거나 앞뒤 공백이 있으면 거부한다.
func LoadProfileFromEnv(identity Identity) (LoadedProfile, error) {
	path, present := os.LookupEnv(ProfileFileEnv)
	if !present || path == "" {
		return LoadedProfile{}, errors.New("workercontract: " + ProfileFileEnv + " is required")
	}

	if path != strings.TrimSpace(path) {
		return LoadedProfile{}, errors.New("workercontract: " + ProfileFileEnv + " must not contain surrounding whitespace")
	}

	sourcePath, hasSource := os.LookupEnv(ProfileSourceFileEnv)
	if hasSource && (sourcePath == "" || sourcePath != strings.TrimSpace(sourcePath)) {
		return LoadedProfile{}, errors.New("workercontract: " + ProfileSourceFileEnv + " must be non-empty without surrounding whitespace")
	}

	loaded, err := LoadProfileFile(path, identity)
	if err != nil {
		return LoadedProfile{}, err
	}

	loaded.sourcePath = sourcePath

	return loaded, nil
}
