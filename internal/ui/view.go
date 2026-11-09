package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/shushanskii/go-lessons/internal/lesson"
	"github.com/shushanskii/go-lessons/internal/runner"
)

// Палитра. Числа — xterm-256, чтобы не зависеть от темы терминала.
var (
	cAccent  = lipgloss.Color("39")
	cOK      = lipgloss.Color("42")
	cFail    = lipgloss.Color("203")
	cMuted   = lipgloss.Color("244")
	cWarning = lipgloss.Color("214")

	styleTitle = lipgloss.NewStyle().Bold(true).Foreground(cAccent)
	styleMuted = lipgloss.NewStyle().Foreground(cMuted)
	styleOK    = lipgloss.NewStyle().Foreground(cOK).Bold(true)
	styleFail  = lipgloss.NewStyle().Foreground(cFail).Bold(true)
	styleWarn  = lipgloss.NewStyle().Foreground(cWarning).Bold(true)
	styleSel   = lipgloss.NewStyle().Foreground(cAccent).Bold(true)

	styleFooter = lipgloss.NewStyle().Foreground(cMuted).Padding(0, 1)
)

// paneStyle рисует рамку: активная панель подсвечена, остальные глухие.
// Без этого невозможно понять, куда пойдут j/k — ровно та претензия, из-за
// которой в прошлой версии был сломан tab.
func paneStyle(active bool) lipgloss.Style {
	col := cMuted
	if active {
		col = cAccent
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(col).
		Padding(0, 1)
}

// ---------- главный вид ----------

func (m Model) View() tea.View {
	var v tea.View
	if m.screen == screenList {
		v = tea.NewView(m.viewList())
	} else {
		v = tea.NewView(m.viewLesson())
	}
	v.AltScreen = true
	return v
}

// ---------- список ----------

func (m Model) viewList() string {
	header := styleTitle.Render("  Go: тренажёр") +
		styleMuted.Render(fmt.Sprintf("   сдано %d из %d", m.store.DoneCount(), len(m.reg.All))) +
		"\n  рабочая папка: " + m.workspace + "\n"

	body := paneStyle(true).
		Width(m.listVP.Width()).
		Height(m.listVP.Height()).
		Render(m.listVP.View())

	footer := styleFooter.Render(m.notice + "\n" +
		"↑↓ выбор · Enter открыть · q выход")

	return lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
}

func (m *Model) renderList() {
	var b strings.Builder
	fmt.Fprintf(&b, "Всего уроков: %d\n\n", len(m.reg.All))

	for i, l := range m.reg.All {
		rec := m.store.Get(l.ID)

		var mark string
		switch {
		case rec.Done:
			mark = styleOK.Render("✓")
		case rec.Attempts > 0:
			mark = styleWarn.Render("…")
		default:
			mark = styleMuted.Render("·")
		}

		num := styleMuted.Render(fmt.Sprintf("%2d.", i+1))
		title := l.Title
		if i == m.idx {
			num = styleSel.Render(fmt.Sprintf("%2d>", i+1))
			title = styleSel.Render(title)
		}

		// «3/5» рядом с названием отвечает на вопрос «на чём я застрял»
		// без захода в урок.
		score := ""
		if !rec.Done && rec.Attempts > 0 && rec.Passed+rec.Failed > 0 {
			score = styleMuted.Render(fmt.Sprintf("  %d/%d", rec.Passed, rec.Passed+rec.Failed))
		}

		flags := ""
		if l.Race {
			flags += styleMuted.Render("  гонки")
		}
		if l.Run {
			flags += styleMuted.Render("  запуск")
		}

		fmt.Fprintf(&b, "%s %s %s%s%s\n", num, mark, title, score, flags)
	}

	m.listVP.SetContent(b.String())
}

// ---------- урок ----------

func (m Model) viewLesson() string {
	l := m.cur
	rec := m.store.Get(l.ID)

	state := styleMuted.Render("не сдан")
	switch {
	case rec.Done:
		state = styleOK.Render("сдан")
	case rec.Passed+rec.Failed > 0:
		state = styleWarn.Render(fmt.Sprintf("%d из %d", rec.Passed, rec.Passed+rec.Failed))
	case rec.Attempts > 0:
		state = styleWarn.Render(fmt.Sprintf("попыток: %d", rec.Attempts))
	}

	status := ""
	switch {
	case m.running:
		status = m.sp.View() + " " + styleMuted.Render("проверяю")
	case m.report != nil && m.report.OK:
		status = styleOK.Render("готово")
	case m.report != nil:
		status = styleFail.Render("ошибки")
	}

	header := lipgloss.NewStyle().Padding(0, 1).Render(
		styleTitle.Render(fmt.Sprintf("%02d · %s", m.idx+1, l.Title)) +
			"   " + state + "   " + status,
	)

	left := paneStyle(m.focus == paneTheory).
		Width(m.theoryVP.Width()).
		Height(m.theoryVP.Height()).
		Render(m.theoryVP.View())

	// Правая колонка: сверху код задания, снизу результат. Оба под одним
	// фокусом-переключателем, но видно всё сразу.
	tasks := paneStyle(m.focus == paneTasks).
		Width(m.tasksVP.Width()).
		Height(m.tasksVP.Height()).
		Render(m.tasksVP.View())

	result := paneStyle(m.focus == paneResult).
		Width(m.resultVP.Width()).
		Height(m.resultVP.Height()).
		Render(m.resultVP.View())

	right := lipgloss.JoinVertical(lipgloss.Left, tasks, result)

	body := lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.NewStyle().Padding(0, 0, 0, 1).Render(left),
		lipgloss.NewStyle().Padding(0, 1, 0, 0).Render(right),
	)

	notice := styleMuted.Render(" " + m.notice)
	footer := styleFooter.Render(
		"r проверить · tab панель · c VS Code · g на ошибку · n следующий · esc список · q выход" +
			styleMuted.Render("   активна: "+m.focus.String()),
	)

	return lipgloss.JoinVertical(lipgloss.Left, header, body, notice, footer)
}

// ---------- панель проверки ----------

func (m *Model) renderResult() {
	if m.width == 0 {
		return
	}
	m.resultVP.SetContent(wrapTo(m.resultText(), m.resultVP.Width()))
	m.resultVP.GotoTop()
}

func (m Model) resultText() string {
	if m.running {
		return m.sp.View() + " собираю и запускаю проверки…\n\n" +
			styleMuted.Render("первый прогон в новой папке дольше — компилятор греет кеш")
	}

	rep := m.report
	if rep == nil {
		var b strings.Builder
		b.WriteString(styleMuted.Render("Проверку ещё не запускали.\n\n"))
		b.WriteString("Напиши код в ")
		b.WriteString(styleTitle.Render(lesson.FileTasks))
		b.WriteString(" и нажми ")
		b.WriteString(styleTitle.Render("r"))
		b.WriteString(".\n\n")
		b.WriteString(styleMuted.Render("c — открыть папку урока в VS Code"))
		return b.String()
	}

	if rep.Fatal != "" {
		return styleFail.Render("Проверка не состоялась\n\n") + rep.Fatal + "\n"
	}

	var b strings.Builder
	b.WriteString(verdictLine(rep) + "\n\n")

	for _, s := range rep.Steps {
		mark := styleFail.Render("✗")
		if s.OK {
			mark = styleOK.Render("✓")
		}
		fmt.Fprintf(&b, "%s %-16s %s\n", mark, s.Name,
			styleMuted.Render(s.Elapsed.Round(10*time.Millisecond).String()))
	}

	// Исход по тестам — то, чего не даёт сырой вывод: видно, сколько уже
	// зелёное, и не кажется ли всё вокруг сломанным.
	if len(rep.Tests) > 0 {
		b.WriteString("\n")
		b.WriteString(indent(testsText(rep.Tests), "  ") + "\n")
	}

	// Для упавших шагов показываем и сырой вывод: парсер разбирает не все
	// форматы, а молчать хуже. Список тестов заменяет вывод тестов, но не
	// заменяет панику или гонку — их терять нельзя.
	for _, s := range rep.Steps {
		raw := strings.TrimSpace(s.Output)
		if !s.OK && raw != "" && needRawOutput(s, rep) {
			b.WriteString("\n" + styleMuted.Render("── вывод "+s.Name+" ──") + "\n")
			b.WriteString(indent(s.Output, "   ") + "\n")
		}
		if s.Name == "запуск" && s.OK && raw != "" {
			b.WriteString("\n" + styleTitle.Render("вывод программы") + "\n")
			b.WriteString(indent(s.Output, "   ") + "\n")
		}
	}

	if p := m.firstProblem(); p != nil {
		b.WriteString("\n" + styleMuted.Render("g — открыть "+p.Location()+" в редакторе"))
	}
	return b.String()
}

func verdictLine(r *runner.Report) string {
	total := r.Passed + r.Failed
	switch {
	case r.OK && total > 0:
		return styleOK.Render(fmt.Sprintf("✓ Все зелёные, %s", plural(int64(total),
			"проверка", "проверки", "проверок")))
	case r.OK:
		return styleOK.Render("✓ Проверки прошли")
	case r.TimedOut:
		return styleFail.Render("✗ Код не завершился за отведённое время")
	case total > 0:
		return styleFail.Render(fmt.Sprintf("✗ %d из %d проходят", r.Passed, total))
	default:
		return styleFail.Render("✗ Проверки красные")
	}
}

// testsText — список тестов с исходами, по алфавиту: порядок в выводе go test
// зависит от того, что раньше упало, а студенту нужно искать конкретный тест.
func testsText(tests []runner.TestResult) string {
	sorted := make([]runner.TestResult, len(tests))
	copy(sorted, tests)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })

	var b strings.Builder
	for _, tr := range sorted {
		var mark string
		switch tr.Status {
		case "pass":
			mark = styleOK.Render("✓")
		case "skip":
			mark = styleMuted.Render("–")
		default:
			mark = styleFail.Render("✗")
		}
		fmt.Fprintf(&b, "%s %s\n", mark, tr.Name)
		if tr.Status == "fail" && strings.TrimSpace(tr.Output) != "" {
			b.WriteString(indent(trimRunLines(tr.Output), "    ") + "\n")
		}
	}
	return b.String()
}

// trimRunLines убирает служебные строки === RUN / --- FAIL: они дублируют то,
// что уже есть в списке.
func trimRunLines(s string) string {
	var keep []string
	for _, line := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "=== RUN") || strings.HasPrefix(t, "--- FAIL") ||
			strings.HasPrefix(t, "--- PASS") || t == "" {
			continue
		}
		keep = append(keep, strings.TrimLeft(line, " \t"))
	}
	return strings.Join(keep, "\n")
}

// needRawOutput — когда сырой вывод шага не заменяется списком тестов.
func needRawOutput(s runner.Step, rep *runner.Report) bool {
	if s.Name != "тесты" {
		return true
	}
	if len(rep.Tests) == 0 {
		return true // парсер не нашёл ни одного теста — показываем как есть
	}
	// Паника, дедлок или гонка живут вне отдельных тестов и в списке теряются.
	return strings.Contains(s.Output, "panic:") ||
		strings.Contains(s.Output, "goroutine") ||
		strings.Contains(strings.ToUpper(s.Output), "DATA RACE")
}

// ---------- мелочи ----------

func indent(s, pad string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		lines[i] = pad + l
	}
	return strings.Join(lines, "\n")
}
