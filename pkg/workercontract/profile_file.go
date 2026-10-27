package workercontract

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

const ProfileFileCheckInterval = 60 * time.Second

// ProfileFileErrorCode는 diagnostics에 공개 가능한 bounded code다.
type ProfileFileErrorCode string

const (
	ProfileFileMissing     ProfileFileErrorCode = "profile_file_missing"
	ProfileFileUnreadable  ProfileFileErrorCode = "profile_file_unreadable"
	ProfileFileTypeInvalid ProfileFileErrorCode = "profile_file_type_invalid"
	ProfileFileChanged     ProfileFileErrorCode = "profile_file_changed"
)

type profileFileError struct {
	code ProfileFileErrorCode
}

func (e profileFileError) Error() string { return "workercontract: worker profile: " + string(e.code) }

// LoadProfileFile은 regular file의 exact bytes를 strict profile로 해석한다.
func LoadProfileFile(path string, identity Identity) (LoadedProfile, error) {
	if path == "" {
		return LoadedProfile{}, profileFileError{code: ProfileFileMissing}
	}

	raw, err := readRegularProfile(path)
	if err != nil {
		return LoadedProfile{}, fmt.Errorf("workercontract: read regular profile: %w", err)
	}

	if len(raw) == 0 || len(raw) > MaxProfileBytes || bytes.HasPrefix(raw, []byte{0xef, 0xbb, 0xbf}) {
		return LoadedProfile{}, errors.New("workercontract: worker profile: invalid file content")
	}

	profile, err := decodeProfile(raw, identity)
	if err != nil {
		return LoadedProfile{}, fmt.Errorf("workercontract: worker profile: %w", err)
	}

	digest := sha256.Sum256(raw)

	return LoadedProfile{Profile: profile, Hash: hex.EncodeToString(digest[:]), path: path}, nil
}

func readRegularProfile(path string) ([]byte, error) {
	file, err := openRegularProfile(path)
	if err != nil {
		return nil, profileOpenError(err)
	}
	defer file.Close()

	raw, err := io.ReadAll(io.LimitReader(file, MaxProfileBytes+1))
	if err != nil {
		return nil, profileFileError{code: ProfileFileUnreadable}
	}

	return raw, nil
}

func profileOpenError(err error) error {
	if fileErr, ok := errors.AsType[profileFileError](err); ok {
		return fileErr
	}

	if errors.Is(err, os.ErrNotExist) {
		return profileFileError{code: ProfileFileMissing}
	}

	return profileFileError{code: ProfileFileUnreadable}
}

// ProfileFileStatus는 loaded bytes와 현재 source의 match 상태만 공개한다.
type ProfileFileStatus struct {
	Match            bool
	CheckedAtEpochMS int64
	ErrorCode        *ProfileFileErrorCode
}

// ProfileFileChecker는 hot reload 없이 같은 profile path의 drift만 관측한다.
type ProfileFileChecker struct {
	mu         sync.RWMutex
	path       string
	loadedHash string
	status     ProfileFileStatus
}

// NewProfileFileChecker는 이미 load된 exact hash를 기준으로 checker를 만든다.
func NewProfileFileChecker(profile LoadedProfile, loadedAt time.Time) *ProfileFileChecker {
	checker := &ProfileFileChecker{
		path:       profile.path,
		loadedHash: profile.Hash,
		status: ProfileFileStatus{
			Match:            true,
			CheckedAtEpochMS: loadedAt.UnixMilli(),
		},
	}

	if profile.sourcePath != "" {
		checker.path = profile.sourcePath
		checker.Check(loadedAt)
	}

	return checker
}

// Check는 현재 bytes를 비교하고 replacement hash를 보존하지 않는다.
func (c *ProfileFileChecker) Check(now time.Time) ProfileFileStatus {
	if c == nil {
		code := ProfileFileMissing
		return ProfileFileStatus{CheckedAtEpochMS: now.UnixMilli(), ErrorCode: &code}
	}

	status := c.observeStatus(now)

	c.mu.Lock()

	c.status = status
	c.mu.Unlock()

	return status
}

func (c *ProfileFileChecker) observeStatus(now time.Time) ProfileFileStatus {
	status := ProfileFileStatus{CheckedAtEpochMS: now.UnixMilli()}

	raw, err := readRegularProfile(c.path)
	if err != nil {
		if fileErr, ok := errors.AsType[profileFileError](err); ok {
			status.ErrorCode = &fileErr.code

			return status
		}

		code := ProfileFileUnreadable

		status.ErrorCode = &code

		return status
	}

	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) == c.loadedHash {
		status.Match = true

		return status
	}

	code := ProfileFileChanged

	status.ErrorCode = &code

	return status
}

// Status는 마지막 완료된 비교 결과를 반환한다.
func (c *ProfileFileChecker) Status() ProfileFileStatus {
	if c == nil {
		return ProfileFileStatus{}
	}

	c.mu.RLock()

	defer c.mu.RUnlock()

	return c.status
}

// Run은 고정 cadence로 drift를 확인하며 context 종료 시 반환한다.
func (c *ProfileFileChecker) Run(ctx context.Context) {
	if c == nil {
		return
	}

	ticker := time.NewTicker(ProfileFileCheckInterval)

	defer ticker.Stop()

	for {
		select {
		case checkedAt := <-ticker.C:
			c.Check(checkedAt)
		case <-ctx.Done():
			return
		}
	}
}
