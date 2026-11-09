package ui

import (
	"fmt"
	"strings"
	"sync"

	"charm.land/glamour/v2"
)

// markdown рендерит теорию урока в терминал.
//
// Рендерер дорогой: он поднимает goldmark и палитру стилей, поэтому держим
// один и переиспользуем, пока не изменилась ширина терминала.
type markdown struct {
	mu    sync.Mutex
	width int
	rnd   *glamour.TermRenderer
	cache map[string]string // текст урока -> готовая картинка в ANSI
}

func newMarkdown() *markdown {
	return &markdown{cache: map[string]string{}}
}

func (m *markdown) render(src string, width int) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if width < 20 {
		width = 20
	}

	if m.rnd != nil && m.width == width {
		if done, ok := m.cache[src]; ok {
			return done, nil
		}
	} else {
		rnd, err := glamour.NewTermRenderer(
			glamour.WithStandardStyle("dark"),
			glamour.WithWordWrap(width),
		)
		if err != nil {
			return "", fmt.Errorf("не собрал рендер markdown: %w", err)
		}
		m.rnd, m.width = rnd, width
		m.cache = map[string]string{}
	}

	done, err := m.rnd.Render(src)
	if err != nil {
		return "", fmt.Errorf("не смог разобрать markdown урока: %w", err)
	}
	done = strings.TrimRight(done, "\n")
	m.cache[src] = done
	return done, nil
}

// drop сбрасывает кеш — например, когда урок исправили на ходу.
func (m *markdown) drop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cache = map[string]string{}
}
