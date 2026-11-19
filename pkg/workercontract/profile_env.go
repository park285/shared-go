package workercontract

import (
	"errors"
	"os"
	"strings"
)

const ProfileFileEnv = "STACK_WORKER_PROFILE_FILE"

const ProfileSourceFileEnv = "STACK_WORKER_PROFILE_SOURCE_FILE"

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
