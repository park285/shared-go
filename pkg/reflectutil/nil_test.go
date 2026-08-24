package reflectutil

import (
	"io"
	"testing"
)

type nilable struct{}

func (*nilable) Read([]byte) (int, error) { return 0, io.EOF }

func TestIsNil(t *testing.T) {
	t.Parallel()

	var (
		nilPointer *nilable
		nilReader  io.Reader
		nilMap     map[string]int
		nilSlice   []int
		nilChan    chan int
		nilFunc    func()
	)

	tests := []struct {
		name  string
		value any
		want  bool
	}{
		{name: "untyped nil", value: nil, want: true},
		{name: "typed nil pointer in interface", value: nilPointer, want: true},
		{name: "nil interface value", value: nilReader, want: true},
		{name: "nil map", value: nilMap, want: true},
		{name: "nil slice", value: nilSlice, want: true},
		{name: "nil chan", value: nilChan, want: true},
		{name: "nil func", value: nilFunc, want: true},
		{name: "non-nil pointer", value: &nilable{}, want: false},
		{name: "empty slice", value: []int{}, want: false},
		{name: "zero struct", value: nilable{}, want: false},
		{name: "zero int", value: 0, want: false},
		{name: "empty string", value: "", want: false},
		{name: "false", value: false, want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := IsNil(tc.value); got != tc.want {
				t.Fatalf("IsNil(%#v) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}
