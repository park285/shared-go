package kakaoformat

import (
	"strconv"
	"strings"
)

type store struct {
	kind   string
	values []string
}

func newStore(kind string) *store {
	return &store{kind: kind}
}

func (s *store) Put(value string) string {
	token := s.token(len(s.values))
	s.values = append(s.values, value)

	return token
}

func (s *store) Restore(text string) string {
	if len(s.values) == 0 {
		return text
	}

	pairs := make([]string, 0, len(s.values)*2)
	for i, value := range s.values {
		pairs = append(pairs, s.token(i), value)
	}

	return strings.NewReplacer(pairs...).Replace(text)
}

func (s *store) token(i int) string {
	return "\x00" + s.kind + strconv.Itoa(i) + "\x00"
}
