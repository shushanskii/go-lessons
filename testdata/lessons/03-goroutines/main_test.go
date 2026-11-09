package main

import (
	"slices"
	"testing"
)

func TestFanOutSquares(t *testing.T) {
	got := FanOut([]int{3, 1, 2})
	want := []int{1, 4, 9}

	slices.Sort(got) // порядок не гарантирован, сравниваем отсортированное
	if !slices.Equal(got, want) {
		t.Errorf("FanOut([]int{3,1,2}) = %v, хотим %v", got, want)
	}
}

// Пустой вход обязан вернуться сразу. Этот тест висит до таймаута, если
// канал никто не закрыл, — именно так выглядит забытый close.
func TestFanOutEmpty(t *testing.T) {
	if got := FanOut(nil); len(got) != 0 {
		t.Errorf("FanOut(nil) = %v, хотим пустой срез", got)
	}
	if got := FanOut([]int{}); len(got) != 0 {
		t.Errorf("FanOut([]int{}) = %v, хотим пустой срез", got)
	}
}

// Ничего не потерялось и не задвоилось.
func TestFanOutCount(t *testing.T) {
	const n = 200
	nums := make([]int, n)
	for i := range nums {
		nums[i] = i
	}

	got := FanOut(nums)
	if len(got) != n {
		t.Errorf("получили %d значений вместо %d", len(got), n)
		return
	}

	slices.Sort(got)
	for i, v := range got {
		if v != i*i {
			t.Fatalf("после сортировки на месте %d лежит %d, хотим %d", i, v, i*i)
		}
	}
}

// Отрицательные числа: квадрат отрицательного не отрицательный.
// Ловит реализацию, которая перепутала n*n на что-то ещё.
func TestFanOutNegatives(t *testing.T) {
	got := FanOut([]int{-3, -2, -1})
	want := []int{1, 4, 9}
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Errorf("FanOut([]int{-3,-2,-1}) = %v, хотим %v", got, want)
	}
}
