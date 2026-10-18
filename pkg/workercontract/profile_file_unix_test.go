//go:build unix

package workercontract_test

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/park285/shared-go/v2/pkg/workercontract"
)

func TestProfileFIFOIsRejectedWithoutWaitingForWriter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile.json")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}

	identity := chatbotIdentity(t)

	finished := make(chan error, 1)

	go func() {
		_, err := workercontract.LoadProfileFile(path, identity)
		finished <- err
	}()

	select {
	case err := <-finished:
		if err == nil {
			t.Fatal("FIFO accepted as a profile")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("profile open waited for a FIFO writer")
	}
}

func TestProfileCheckerRejectsReplacementSymlink(t *testing.T) {
	raw, err := os.ReadFile(validProfilePath())
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "profile.json")
	target := filepath.Join(dir, "same-bytes.json")

	for _, name := range []string{path, target} {
		if writeErr := os.WriteFile(name, raw, 0o600); writeErr != nil { //nolint:gosec // 테스트가 만든 임시 디렉터리 경로만 쓴다.
			t.Fatal(writeErr)
		}
	}

	loaded, err := workercontract.LoadProfileFile(path, chatbotIdentity(t))
	if err != nil {
		t.Fatal(err)
	}

	checker := workercontract.NewProfileFileChecker(loaded, time.Unix(100, 0))

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}

	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}

	status := checker.Check(time.Unix(101, 0))
	if status.Match || status.ErrorCode == nil || *status.ErrorCode != workercontract.ProfileFileTypeInvalid {
		t.Fatalf("identical bytes behind replacement symlink accepted: %+v", status)
	}
}
