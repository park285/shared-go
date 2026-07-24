package json

import (
	"errors"
	"io"

	"github.com/bytedance/sonic"
)

type Encoder = sonic.Encoder

// RawMessage는 지연 디코딩을 위한 raw JSON 바이트를 보관합니다.
type RawMessage []byte

// MarshalJSON은 raw payload를 그대로 반환합니다.
func (m RawMessage) MarshalJSON() ([]byte, error) {
	if m == nil {
		return []byte("null"), nil
	}
	return m, nil
}

// UnmarshalJSON은 입력 바이트를 복사해 저장합니다.
func (m *RawMessage) UnmarshalJSON(data []byte) error {
	if m == nil {
		return errors.New("json.RawMessage: UnmarshalJSON on nil pointer")
	}
	*m = append((*m)[:0], data...)
	return nil
}

// CopyString: 디코드 string이 입력 버퍼를 alias하지 않게 복사 (버퍼 재사용 시 corruption 방지).
// ValidateString: 표준 라이브러리처럼 unescaped 제어문자(U+0000~U+001F)를 거부.
var api = sonic.Config{
	CopyString:     true,
	ValidateString: true,
}.Froze()

func Marshal(v any) ([]byte, error) {
	return api.Marshal(v)
}

func Unmarshal(data []byte, v any) error {
	return api.Unmarshal(data, v)
}

func NewEncoder(w io.Writer) Encoder {
	return api.NewEncoder(w)
}

func NewDecoder(r io.Reader) sonic.Decoder {
	return api.NewDecoder(r)
}

func MarshalIndent(v any, prefix, indent string) ([]byte, error) {
	return api.MarshalIndent(v, prefix, indent)
}

func Valid(data []byte) bool {
	return api.Valid(data)
}
