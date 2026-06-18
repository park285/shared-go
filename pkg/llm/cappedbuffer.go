package llm

import (
	"bytes"
	"fmt"
)

type cappedBuffer struct {
	buf   bytes.Buffer
	limit int
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if b == nil {
		return len(p), nil
	}
	if b.limit <= 0 {
		return len(p), nil
	}
	remaining := b.limit - b.buf.Len()
	if remaining > 0 {
		var err error
		if len(p) > remaining {
			_, err = b.buf.Write(p[:remaining])
		} else {
			_, err = b.buf.Write(p)
		}
		if err != nil {
			return 0, fmt.Errorf("write capped buffer: %w", err)
		}
	}
	return len(p), nil
}

func (b *cappedBuffer) String() string {
	if b == nil {
		return ""
	}
	return b.buf.String()
}
