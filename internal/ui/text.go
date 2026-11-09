package ui

import (
	"fmt"

	"github.com/charmbracelet/x/ansi"
)

// plural выбирает русскую множественную форму.
//
// Читает число целиком, а не последнюю цифру: иначе «11 урок» вместо
// «11 уроков».
func plural(n int64, one, few, many string) string {
	if n < 0 {
		n = -n
	}
	if n >= 11 && n <= 14 {
		return many
	}
	switch n % 10 {
	case 1:
		return one
	case 2, 3, 4:
		return few
	default:
		return many
	}
}

// lessonsWord — «1 урок», «2 урока», «5 уроков».
func lessonsWord(n int) string {
	return fmt.Sprintf("%d %s", n, plural(int64(n), "урок", "урока", "уроков"))
}

// wrapTo переносит длинные строки по ширине панели.
//
// viewport сам не переносит — он режет, и вывод go test обрывался на середине
// слова: «хотим 5» превращалось в «хоти». ansi.Wrap знает про escape-коды,
// поэтому переносит раскрашенный текст, не ломая стили.
func wrapTo(s string, width int) string {
	if width < 12 {
		return s
	}
	return ansi.Wrap(s, width, " -\t")
}
