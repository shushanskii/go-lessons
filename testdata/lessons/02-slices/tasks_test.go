package slices

import (
	"reflect"
	"testing"
)

func TestEvens(t *testing.T) {
	tests := []struct {
		in   []int
		want []int
	}{
		{nil, nil},
		{[]int{}, nil},
		{[]int{1, 2, 3, 4}, []int{2, 4}},
		{[]int{2, 4, 6}, []int{2, 4, 6}},
		{[]int{1, 3, 5}, nil},
		{[]int{-2, -1, 0, 1}, []int{-2, 0}}, // ноль чётный
	}
	for _, tt := range tests {
		if got := Evens(tt.in); !reflect.DeepEqual(got, tt.want) && !(len(got) == 0 && len(tt.want) == 0) {
			t.Errorf("Evens(%v) = %v, хотим %v", tt.in, got, tt.want)
		}
	}
}

// Evens не должен править вход — иначе вызывающий теряет данные.
func TestEvensKeepsInput(t *testing.T) {
	in := []int{1, 2, 3, 4, 5, 6}
	Evens(in)
	want := []int{1, 2, 3, 4, 5, 6}
	if !reflect.DeepEqual(in, want) {
		t.Errorf("Evens испортил вход: %v, было %v", in, want)
	}
}

func TestDedup(t *testing.T) {
	tests := []struct {
		in   []string
		want []string
	}{
		{nil, nil},
		{[]string{"а", "б", "а", "в", "б"}, []string{"а", "б", "в"}},
		{[]string{"раз"}, []string{"раз"}},
		{[]string{"x", "x", "x"}, []string{"x"}},
	}
	for _, tt := range tests {
		if got := Dedup(tt.in); !reflect.DeepEqual(got, tt.want) && !(len(got) == 0 && len(tt.want) == 0) {
			t.Errorf("Dedup(%v) = %v, хотим %v", tt.in, got, tt.want)
		}
	}
}

func TestReverse(t *testing.T) {
	tests := []struct {
		in   []int
		want []int
	}{
		{nil, nil},
		{[]int{7}, []int{7}},
		{[]int{1, 2}, []int{2, 1}},
		{[]int{1, 2, 3, 4}, []int{4, 3, 2, 1}},
		{[]int{1, 2, 3, 4, 5}, []int{5, 4, 3, 2, 1}}, // нечётная длина: середина на месте
	}
	for _, tt := range tests {
		Reverse(tt.in)
		if !reflect.DeepEqual(tt.in, tt.want) {
			t.Errorf("Reverse изменил срез в %v, хотим %v", tt.in, tt.want)
		}
	}
}

// Отдельная проверка на «окно в массив»: разворот должен дойти до вызывающего.
func TestReverseIsInPlace(t *testing.T) {
	orig := []int{1, 2, 3}
	alias := orig
	Reverse(alias)
	if !reflect.DeepEqual(orig, []int{3, 2, 1}) {
		t.Errorf("Reverse сделал копию вместо разворота на месте: orig = %v", orig)
	}
}
