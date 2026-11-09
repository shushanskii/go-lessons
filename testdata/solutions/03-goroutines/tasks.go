package main

import (
	"fmt"
	"sync"
)

// FanOut запускает по горутине на каждое число и собирает квадраты через канал.
//
// Порядок в ответе не важен: горутины завершаются кто раньше.
//
// Что нужно дописать:
//
//  1. Для каждого nums[i] запусти горутину: wg.Add(1) до запуска,
//     defer wg.Done() внутри, посчитанный квадрат отправь в squares.
//
//  2. Закрой squares, когда дождёшься все горутины. Пока канал не закрыт,
//     цикл range ниже не завершится никогда — проверка будет висеть
//     и упадёт по таймауту, а не по ошибке.
//
// Проверка гоняет тесты с -race: если начнёшь складывать квадраты в общий
// срез напрямую, она это увидит.
func FanOut(nums []int) []int {
	squares := make(chan int)
	var wg sync.WaitGroup

	for _, n := range nums {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			squares <- n * n
		}(n)
	}

	go func() {
		wg.Wait()
		close(squares)
	}()

	var out []int
	for v := range squares {
		out = append(out, v)
	}
	return out
}

func main() {
	fmt.Println("Квадраты 1..5:", FanOut([]int{1, 2, 3, 4, 5}))
}
