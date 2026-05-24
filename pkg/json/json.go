package json

import (
	"errors"
	"io"
	"strconv"

	"github.com/bytedance/sonic"
	"github.com/bytedance/sonic/decoder"
)

type (
	// sonic은 전용 오류 타입을 반환하므로 별칭으로 노출해 표준 라이브러리 호환 지점을 유지한다.
	SyntaxError        = decoder.SyntaxError
	UnmarshalTypeError = decoder.MismatchTypeError

	Encoder = sonic.Encoder
	Decoder = sonic.Decoder
)

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

// Number는 JSON 숫자 리터럴 문자열을 보관합니다.
type Number string

// String은 숫자 문자열을 반환합니다.
func (n Number) String() string {
	return string(n)
}

// Float64는 숫자 문자열을 float64로 변환합니다.
func (n Number) Float64() (float64, error) {
	return strconv.ParseFloat(string(n), 64)
}

// Int64는 숫자 문자열을 int64로 변환합니다.
func (n Number) Int64() (int64, error) {
	return strconv.ParseInt(string(n), 10, 64)
}

var api = sonic.ConfigDefault

func Marshal(v any) ([]byte, error) {
	return api.Marshal(v)
}

func Unmarshal(data []byte, v any) error {
	return api.Unmarshal(data, v)
}

func NewEncoder(w io.Writer) Encoder {
	return api.NewEncoder(w)
}

func NewDecoder(r io.Reader) Decoder {
	return api.NewDecoder(r)
}

func MarshalIndent(v any, prefix, indent string) ([]byte, error) {
	return api.MarshalIndent(v, prefix, indent)
}

func Valid(data []byte) bool {
	return api.Valid(data)
}
