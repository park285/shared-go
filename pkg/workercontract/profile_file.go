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

func (e profileFileError) Error() string { return "worker profile: " + string(e.code) }

// LoadProfileFile은 regular file의 exact bytes를 strict profile로 해석한다.
func LoadProfileFile(path string, identity Identity) (LoadedProfile, error) {
	if path == "" {
		return LoadedProfile{}, profileFileError{code: ProfileFileMissing}
	}
	raw, err := readRegularProfile(path)
	if err != nil {
		return LoadedProfile{}, err
	}
	profile, err := decodeProfile(raw, identity)
	if err != nil {
		return LoadedProfile{}, fmt.Errorf("worker profile: %w", err)
	}
	digest := sha256.Sum256(raw)
	return LoadedProfile{Profile: profile, Hash: hex.EncodeToString(digest[:]), path: path}, nil
}

func readRegularProfile(path string) ([]byte, error) {
	before, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, profileFileError{code: ProfileFileMissing}
		}
		return nil, profileFileError{code: ProfileFileUnreadable}
	}
	if !before.Mode().IsRegular() {
		return nil, profileFileError{code: ProfileFileTypeInvalid}
	}
	// #nosec G304 -- path is the explicit operator-owned profile source; Lstat/File.Stat inode checks reject substitution.
	file, err := os.Open(path)
	if err != nil {
		return nil, profileFileError{code: ProfileFileUnreadable}
	}
	defer file.Close()
	after, lstatErr := os.Lstat(path)
	opened, statErr := file.Stat()
	if lstatErr != nil || statErr != nil || !after.Mode().IsRegular() || !opened.Mode().IsRegular() ||
		!os.SameFile(before, after) || !os.SameFile(after, opened) {
		return nil, profileFileError{code: ProfileFileTypeInvalid}
	}
	raw, err := io.ReadAll(io.LimitReader(file, MaxProfileBytes+1))
	if err != nil {
		return nil, profileFileError{code: ProfileFileUnreadable}
	}
	if len(raw) == 0 || len(raw) > MaxProfileBytes || bytes.HasPrefix(raw, []byte{0xef, 0xbb, 0xbf}) {
		return nil, profileFileError{code: ProfileFileTypeInvalid}
	}
	return raw, nil
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
	return &ProfileFileChecker{
		path:       profile.path,
		loadedHash: profile.Hash,
		status: ProfileFileStatus{
			Match:            true,
			CheckedAtEpochMS: loadedAt.UnixMilli(),
		},
	}
}

// Check는 현재 bytes를 비교하고 replacement hash를 보존하지 않는다.
func (c *ProfileFileChecker) Check(now time.Time) ProfileFileStatus {
	if c == nil {
		code := ProfileFileMissing
		return ProfileFileStatus{CheckedAtEpochMS: now.UnixMilli(), ErrorCode: &code}
	}
	raw, err := readRegularProfile(c.path)
	status := ProfileFileStatus{CheckedAtEpochMS: now.UnixMilli()}
	if err != nil {
		if fileErr, ok := errors.AsType[profileFileError](err); ok {
			status.ErrorCode = &fileErr.code
		} else {
			code := ProfileFileUnreadable
			status.ErrorCode = &code
		}
	} else {
		digest := sha256.Sum256(raw)
		if hex.EncodeToString(digest[:]) == c.loadedHash {
			status.Match = true
		} else {
			code := ProfileFileChanged
			status.ErrorCode = &code
		}
	}
	c.mu.Lock()
	c.status = status
	c.mu.Unlock()
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
