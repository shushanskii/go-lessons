// Package ui — терминальный интерфейс тренажёра.
package ui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/shushanskii/go-lessons/internal/lesson"
	"github.com/shushanskii/go-lessons/internal/progress"
	"github.com/shushanskii/go-lessons/internal/runner"
)

type screen int

const (
	screenList screen = iota
	screenLesson
)

// pane — панель, которая слушает клавиши прокрутки.
//
// Порядок важен: по нему крутится tab.
type pane int

const (
	paneTheory pane = iota
	paneTasks
	paneResult
)

func (p pane) String() string {
	switch p {
	case paneTasks:
		return "задание"
	case paneResult:
		return "проверка"
	default:
		return "теория"
	}
}

var paneOrder = []pane{paneTheory, paneTasks, paneResult}

// Сообщения от фоновых дел.
type (
	checkDoneMsg struct{ rep *runner.Report }
	editorMsg    struct {
		err error
		// Target — что открывали: папка урока или файл со строкой.
		Target string
	}
)

// Модель приложения.
type Model struct {
	reg       *lesson.Registry
	store     *progress.Store
	workspace string
	md        *markdown

	width, height int

	screen screen
	idx    int // курсор в списке уроков
	cur    *lesson.Lesson

	listVP   viewport.Model
	theoryVP viewport.Model
	tasksVP  viewport.Model
	resultVP viewport.Model
	sp       spinner.Model

	focus   pane
	running bool
	report  *runner.Report
	notice  string
}

// New собирает стартовую модель.
func New(reg *lesson.Registry, store *progress.Store, workspace string) Model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(cAccent)

	m := Model{
		reg:       reg,
		store:     store,
		workspace: workspace,
		md:        newMarkdown(),
		screen:    screenList,
		sp:        s,
		listVP:    viewport.New(),
		theoryVP:  viewport.New(),
		tasksVP:   viewport.New(),
		resultVP:  viewport.New(),
		notice:    "выбери урок и нажми Enter",
	}
	return m
}

func (m Model) Init() tea.Cmd { return nil }

// ---------- Update ----------

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.sp, cmd = m.sp.Update(msg)
		return m, cmd

	case checkDoneMsg:
		return m.afterCheck(msg.rep)

	case editorMsg:
		if msg.err != nil {
			m.notice = fmt.Sprintf("не открылся %s: %v", msg.Target, msg.err)
		} else {
			m.notice = "открыл " + msg.Target + " — правь код и возвращайся на r"
		}
		m.reloadTasks()
		return m, nil
	}

	if m.screen == screenList {
		return m.updateList(msg)
	}
	return m.updateLesson(msg)
}

func (m Model) afterCheck(rep *runner.Report) (tea.Model, tea.Cmd) {
	m.running = false
	m.report = rep
	m.reloadTasks() // код на диске мог измениться, пока шла проверка

	if rep.Fatal != "" {
		m.notice = rep.Fatal
		m.renderResult()
		return m, nil
	}

	if m.cur != nil {
		if err := m.store.Pass(m.cur.ID, rep.OK, rep.Passed, rep.Failed); err != nil {
			m.notice = "не смог записать прогресс: " + err.Error()
		}
	}

	switch {
	case rep.OK:
		m.notice = fmt.Sprintf("сдано за %s — следующий урок по n",
			rep.Elapsed.Round(10*time.Millisecond))
	case rep.TimedOut:
		m.notice = "код не завершился за отведённое время — похоже на бесконечный цикл или незакрытый канал"
	case rep.Failed > 0:
		m.notice = fmt.Sprintf("падают %d %s — g откроет редактор на первой ошибке",
			rep.Failed, plural(int64(rep.Failed), "тест", "теста", "тестов"))
	default:
		m.notice = "проверки красные — читай разбор сверху"
	}
	m.renderResult()
	return m, nil
}

// ---------- список ----------

func (m Model) updateList(msg tea.Msg) (tea.Model, tea.Cmd) {
	if km, ok := msg.(tea.KeyPressMsg); ok {
		switch km.String() {
		case "q", "esc", "ctrl+c":
			return m, tea.Quit
		case "j", "down":
			if m.idx < len(m.reg.All)-1 {
				m.idx++
				m.syncListCursor()
			}
			return m, nil
		case "k", "up":
			if m.idx > 0 {
				m.idx--
				m.syncListCursor()
			}
			return m, nil
		case "enter":
			return m.openLesson(m.reg.All[m.idx])
		}
	}

	var cmd tea.Cmd
	m.listVP, cmd = m.listVP.Update(msg)
	return m, cmd
}

func (m Model) openLesson(l *lesson.Lesson) (tea.Model, tea.Cmd) {
	created, err := l.Unpack(m.workspace)
	if err != nil {
		m.notice = "не разложил файлы урока: " + err.Error()
		return m, nil
	}

	m.cur = l
	m.screen = screenLesson
	m.focus = paneTheory
	m.report = nil
	m.layout()

	if created {
		m.notice = "урок разложен в " + l.Dir(m.workspace)
	} else {
		m.notice = "правь " + lesson.FileTasks + " в редакторе, проверяй на r"
	}
	return m, nil
}

// ---------- урок ----------

func (m Model) updateLesson(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Поле ввода в интерфейсе нет, значит любая клавиша свободна: r работает
	// всегда, независимо от того, на какой панели курсор.
	if km, ok := msg.(tea.KeyPressMsg); ok {
		switch km.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "q":
			return m, tea.Quit
		case "esc", "backspace":
			m.cur, m.screen = nil, screenList
			m.layout()
			m.syncListCursor()
			m.notice = "назад к списку — выбери следующий урок"
			return m, nil
		case "r":
			return m.startCheck()
		case "tab", "l":
			m.cycleFocus(1)
			return m, nil
		case "shift+tab", "h":
			m.cycleFocus(-1)
			return m, nil
		case "1":
			return m.setFocus(paneTheory)
		case "2":
			return m.setFocus(paneTasks)
		case "3":
			return m.setFocus(paneResult)
		case "c":
			return m.openInVSCode(m.cur.Dir(m.workspace), "папку урока")
		case "g":
			return m.gotoProblem()
		case "n":
			if m.idx+1 < len(m.reg.All) {
				m.idx++
				return m.openLesson(m.reg.All[m.idx])
			}
			m.notice = "это последний урок"
			return m, nil
		case "p":
			if m.idx > 0 {
				m.idx--
				return m.openLesson(m.reg.All[m.idx])
			}
			return m, nil
		case "j", "down":
			m.scrollFocused(1)
			return m, nil
		case "k", "up":
			m.scrollFocused(-1)
			return m, nil
		case "pgdown", "f":
			m.focused().PageDown()
			return m, nil
		case "pgup", "b":
			m.focused().PageUp()
			return m, nil
		case "home":
			m.focused().GotoTop()
			return m, nil
		case "end", "G":
			m.focused().GotoBottom()
			return m, nil
		}
	}

	// Мышь и прочее — в панель под курсором.
	var cmd tea.Cmd
	switch m.focus {
	case paneTasks:
		m.tasksVP, cmd = m.tasksVP.Update(msg)
	case paneResult:
		m.resultVP, cmd = m.resultVP.Update(msg)
	default:
		m.theoryVP, cmd = m.theoryVP.Update(msg)
	}
	return m, cmd
}

func (m Model) setFocus(p pane) (tea.Model, tea.Cmd) {
	m.focus = p
	if p == paneTasks {
		m.reloadTasks()
	}
	return m, nil
}

func (m *Model) cycleFocus(step int) {
	for i, p := range paneOrder {
		if p == m.focus {
			n := (i + step + len(paneOrder)) % len(paneOrder)
			m.focus = paneOrder[n]
			break
		}
	}
	if m.focus == paneTasks {
		m.reloadTasks()
	}
}

func (m *Model) focused() *viewport.Model {
	switch m.focus {
	case paneTasks:
		return &m.tasksVP
	case paneResult:
		return &m.resultVP
	default:
		return &m.theoryVP
	}
}

func (m *Model) scrollFocused(n int) { m.focused().ScrollDown(n) }

// startCheck запускает проверки фоном: интерфейс не должен замирать
// на полсекунды, пока идёт сборка.
func (m Model) startCheck() (tea.Model, tea.Cmd) {
	if m.running {
		m.notice = "проверка уже идёт"
		return m, nil
	}
	if m.cur == nil {
		return m, nil
	}

	m.running = true
	m.notice = "проверяю…"
	m.renderResult()

	dir := m.cur.Dir(m.workspace)
	wantRun, wantRace := m.cur.Run, m.cur.Race
	return m, func() tea.Msg {
		rep := runner.Check(context.Background(), runner.Options{
			Dir:     dir,
			AlsoRun: wantRun,
			Race:    wantRace,
		})
		return checkDoneMsg{rep: rep}
	}
}

// ---------- VS Code ----------

// openInVSCode открывает путь в VS Code.
//
// Не ждём завершения: code обычно возвращает управление сразу, и ждать его —
// значит подвесить интерфейс.
func (m Model) openInVSCode(path, what string) (tea.Model, tea.Cmd) {
	bin, args, err := editorCommand(nil, path)
	if err != nil {
		m.notice = err.Error() + " — путь лежит в " + path
		return m, nil
	}
	m.notice = "открываю " + what + "…"
	return m, spawnEditor(bin, args, what)
}

// gotoProblem уводит редактор ровно на ту строку, где всё сломалось.
func (m Model) gotoProblem() (tea.Model, tea.Cmd) {
	p := m.firstProblem()
	if p == nil {
		m.notice = "пока не на что указывать: запусти проверку на r"
		return m, nil
	}

	// Go печатает путь относительно папки урока.
	full := p.File
	if !filepath.IsAbs(full) {
		full = filepath.Join(m.cur.Dir(m.workspace), p.File)
	}
	if _, err := os.Stat(full); err != nil {
		m.notice = "файл " + p.File + " не найден — возможно, его переименовали"
		return m, nil
	}

	bin, args, err := editorCommand([]string{"--goto"}, full)
	if err != nil {
		m.notice = err.Error() + " — ошибка лежит в " + p.String()
		return m, nil
	}
	if p.Line > 0 {
		args = append(args, fmt.Sprintf("%s:%d", full, p.Line))
	} else {
		args = append(args, full)
	}

	m.notice = "открываю " + p.Location()
	return m, spawnEditor(bin, args, p.Location())
}

// firstProblem — куда смотреть в первую очередь.
func (m Model) firstProblem() *runner.Problem {
	if m.report == nil {
		return nil
	}

	// Сначала не-тест: студент чинит свой код, а не проверку. Тест упоминаем
	// только когда в его коде указывать не на что.
	var fallback *runner.Problem

	// С шагами идём с конца: тесты важнее разбора, который часто ругается
	// на то же место общими словами.
	for i := len(m.report.Steps) - 1; i >= 0; i-- {
		probs := m.report.Steps[i].Problems
		for j := range probs {
			p := &probs[j]
			if !strings.HasSuffix(p.File, "_test.go") {
				return p
			}
			if fallback == nil {
				fallback = p
			}
		}
	}
	return fallback
}

// editorCommand собирает запуск редактора: сначала VS Code, потом $EDITOR.
func editorCommand(extra []string, path string) (string, []string, error) {
	if bin, err := exec.LookPath("code"); err == nil {
		args := append(append([]string{}, extra...), path)
		return bin, args, nil
	}
	if ed := os.Getenv("EDITOR"); ed != "" {
		argv := strings.Fields(ed)
		return argv[0], append(append(argv[1:], extra...), path), nil
	}
	if ed := os.Getenv("VISUAL"); ed != "" {
		argv := strings.Fields(ed)
		return argv[0], append(append(argv[1:], extra...), path), nil
	}
	return "", nil, fmt.Errorf("не нашёл редактор: нет ни code, ни EDITOR")
}

func spawnEditor(bin string, args []string, target string) tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command(bin, args...)
		if err := cmd.Start(); err != nil {
			return editorMsg{err: err, Target: target}
		}
		// Дожидаемся в этой же горутине, чтобы не оставить зомби-процесс.
		_ = cmd.Wait()
		return editorMsg{err: nil, Target: target}
	}
}

// ---------- содержимое панелей ----------

func (m *Model) reloadTasks() {
	if m.cur == nil || m.width == 0 {
		return
	}
	src, err := m.cur.Read(m.workspace, lesson.FileTasks)
	if err != nil {
		m.tasksVP.SetContent("нет файла " + lesson.FileTasks + ":\n" + err.Error())
		return
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", styleMuted.Render(m.cur.Dir(m.workspace)))
	b.WriteString(src)

	if tests, err := m.cur.Read(m.workspace, lesson.FileTests); err == nil {
		b.WriteString("\n\n")
		b.WriteString(styleMuted.Render("── "+lesson.FileTests+" — их лучше не трогать ──") + "\n")
		b.WriteString(tests)
	}
	m.tasksVP.SetContent(wrapTo(b.String(), m.tasksVP.Width()))
}

// ---------- раскладка ----------

func (m *Model) layout() {
	if m.width == 0 || m.height == 0 {
		return
	}
	const (
		border = 2 // левая и правая рамки
		pad    = 2 // внутренние отступы
		chrome = 3 // шапка, подсказка, подвал
	)
	bodyH := max(m.height-chrome, 5)
	inner := max(bodyH-border, 1)

	if m.screen == screenList {
		m.listVP.SetWidth(m.width - border - pad)
		m.listVP.SetHeight(inner)
		m.renderList()
		return
	}

	leftW := m.width * 46 / 100
	rightW := m.width - leftW
	tasksH := max(bodyH*32/100, 6)
	resultH := max(bodyH-tasksH, 4)

	m.theoryVP.SetWidth(leftW - border - pad)
	m.theoryVP.SetHeight(inner)

	tasksInner := max(tasksH-border, 1)
	resultInner := max(resultH-border, 1)

	m.tasksVP.SetWidth(rightW - border - pad)
	m.tasksVP.SetHeight(tasksInner)
	m.resultVP.SetWidth(rightW - border - pad)
	m.resultVP.SetHeight(resultInner)

	m.setTheory()
	m.reloadTasks()
	m.renderResult()
}

func (m *Model) setTheory() {
	if m.cur == nil || m.width == 0 {
		return
	}
	rendered, err := m.md.render(m.cur.Theory, m.theoryVP.Width())
	if err != nil {
		rendered = "# не смог отрендерить урок\n\n" + err.Error()
	}
	m.theoryVP.SetContent(rendered)
	m.theoryVP.GotoTop()
}

func (m *Model) syncListCursor() {
	off := m.idx
	if h := m.listVP.Height(); h > 0 && off >= m.listVP.YOffset()+h {
		m.listVP.SetYOffset(off - h + 1)
	}
	if off < m.listVP.YOffset() {
		m.listVP.SetYOffset(off)
	}
	m.renderList()
}
