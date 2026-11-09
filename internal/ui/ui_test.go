package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/shushanskii/go-lessons/internal/lesson"
	"github.com/shushanskii/go-lessons/internal/progress"
	"github.com/shushanskii/go-lessons/internal/runner"
)

func newTestModel(t *testing.T) (Model, string) {
	t.Helper()

	abs, err := filepath.Abs(filepath.Join("..", "..", "testdata", "lessons"))
	if err != nil {
		t.Fatal(err)
	}
	reg, err := lesson.Load(os.DirFS(abs))
	if err != nil {
		t.Fatalf("не смог прочитать уроки: %v", err)
	}

	ws := t.TempDir()
	store, err := progress.Open(filepath.Join(ws, ".progress.json"))
	if err != nil {
		t.Fatal(err)
	}

	m := New(reg, store, ws)
	// Размер терминала обычно присылает настоящий терминал; в тесте задаём сами,
	// иначе вьюха пустая.
	m = update(t, m, tea.WindowSizeMsg{Width: 120, Height: 40}).(Model)
	return m, ws
}

func update(t *testing.T, m Model, msg tea.Msg) tea.Model {
	t.Helper()
	updated, _ := m.Update(msg)
	return updated
}

// key — печатная клавиша. Для служебных она не годится: у [tea.KeyPressMsg]
// метод String() строится из Code, но если заполнен ещё и Text, выигрывает он.
// То есть key(tea.KeyTab) даст "\t", а обработчик ждёт "tab".
func key(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: r, Text: string(r)} }

// keyCode — служебный ключ: Enter, Tab, Escape, стрелки.
func keyCode(c rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: c} }

// content возвращает экран без ANSI-кодов: проверять текст по нему одно,
// а разбирать escape-последовательности глазами — совсем другое.
func content(t *testing.T, m Model) string {
	t.Helper()
	v := m.View()
	if v.Content == "" {
		t.Fatal("View вернул пустой контент")
	}
	return ansi.Strip(v.Content)
}

// runCheckOnscreen нажимает r и возвращает модель с готовым отчётом.
func runCheckOnscreen(t *testing.T, m Model) Model {
	t.Helper()
	updated, cmd := m.Update(key('r'))
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("r не вернул команду запуска проверки")
	}
	return update(t, m, cmd()).(Model)
}

func openFirstLesson(t *testing.T, m Model) Model {
	t.Helper()
	return update(t, m, keyCode(tea.KeyEnter)).(Model)
}

// ---------- список ----------

func TestListShowsLessons(t *testing.T) {
	m, ws := newTestModel(t)
	got := content(t, m)

	for _, want := range []string{"тренажёр", "Переменные", "Срезы", "Горутины", ws} {
		if !strings.Contains(got, want) {
			t.Errorf("в списке нет %q\n----\n%s", want, got)
		}
	}
	if !strings.Contains(got, "сдано 0 из 3") {
		t.Errorf("счётчик сданных не на месте: %q", got)
	}
}

func TestListNavigation(t *testing.T) {
	m, _ := newTestModel(t)

	m = update(t, m, keyCode(tea.KeyDown)).(Model)
	if !strings.Contains(content(t, m), "2>") {
		t.Error("стрелка вниз не поставила курсор на второй урок")
	}

	// За нижнюю границу выходить нельзя.
	for i := 0; i < 9; i++ {
		m = update(t, m, keyCode(tea.KeyDown)).(Model)
	}
	m = openFirstLesson(t, m)
	if m.cur.ID != "03-goroutines" {
		t.Errorf("ушли за конец списка: открылся %s", m.cur.ID)
	}

	// И за верхнюю.
	m = update(t, m, keyCode(tea.KeyEscape)).(Model)
	for i := 0; i < 9; i++ {
		m = update(t, m, keyCode(tea.KeyUp)).(Model)
	}
	m = openFirstLesson(t, m)
	if m.cur.ID != "01-basics" {
		t.Errorf("ушли за начало списка: открылся %s", m.cur.ID)
	}
}

// ---------- урок ----------

func TestOpenLessonShowsTheoryAndUnpacksFiles(t *testing.T) {
	m, ws := newTestModel(t)
	m = openFirstLesson(t, m)
	got := content(t, m)

	if !strings.Contains(got, "Переменные") {
		t.Errorf("похоже, не открылся первый урок\n%s", got)
	}
	if !strings.Contains(got, "r проверить") {
		t.Errorf("в подсказках нет клавиши проверки\n%s", got)
	}

	for _, name := range []string{lesson.FileTasks, lesson.FileTests, "go.mod", "lesson.md"} {
		if _, err := os.Stat(filepath.Join(ws, "lessons", "01-basics", name)); name != "lesson.md" && err != nil {
			t.Errorf("файл %s не разложен: %v", name, err)
		}
	}
	// Теория остаётся в бинарнике, в рабочую папку её класть не нужно.
	if _, err := os.Stat(filepath.Join(ws, "lessons", "01-basics", "lesson.md")); err == nil {
		t.Error("lesson.md попал в рабочую папку — там ему не место")
	}
}

func TestEscReturnsToList(t *testing.T) {
	m, _ := newTestModel(t)
	m = openFirstLesson(t, m)
	m = update(t, m, keyCode(tea.KeyEscape)).(Model)

	if !strings.Contains(content(t, m), "назад к списку") {
		t.Error("Escape не вернул к списку")
	}
}

func TestNextPrevLesson(t *testing.T) {
	m, _ := newTestModel(t)
	m = openFirstLesson(t, m)

	m = update(t, m, key('n')).(Model)
	if m.cur.ID != "02-slices" {
		t.Errorf("n перешёл на %s, ждали 02-slices", m.cur.ID)
	}
	m = update(t, m, key('p')).(Model)
	if m.cur.ID != "01-basics" {
		t.Errorf("p вернулся на %s, ждали 01-basics", m.cur.ID)
	}
}

// ---------- фокус: ровно тот баг, из-за которого сломался tab ----------

func TestFocusCyclesThroughAllPanes(t *testing.T) {
	m, _ := newTestModel(t)
	m = openFirstLesson(t, m)

	want := []pane{paneTasks, paneResult, paneTheory}
	for i, expect := range want {
		m = update(t, m, keyCode(tea.KeyTab)).(Model)
		if m.focus != expect {
			t.Fatalf("tab #%d: фокус %v, ждали %v", i+1, m.focus, expect)
		}
	}

	// Обратный ход.
	m = update(t, m, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}).(Model)
	if m.focus != paneResult {
		t.Errorf("shift+tab: фокус %v, ждали %v", m.focus, paneResult)
	}
}

// Активная панель должна быть видна: без этого непонятно, куда пойдут j/k.
func TestActivePaneIsMarked(t *testing.T) {
	m, _ := newTestModel(t)
	m = openFirstLesson(t, m)

	if !strings.Contains(content(t, m), "активна: теория") {
		t.Error("в подвале не сказано, какая панель активна")
	}

	m = update(t, m, key('3')).(Model)
	if !strings.Contains(content(t, m), "активна: проверка") {
		t.Error("клавиша 3 не перевела фокус на проверку")
	}

	// Рамка активной панели рисуется цветом акцента, остальные — глухим.
	// Считаем, сколько раз в кадре встречается цвет акцента: ровно один
	// заголовок плюс одна рамка, но не три рамки.
	if n := strings.Count(m.View().Content, accentSequence); n > 4 {
		t.Errorf("подсвеченных акцентом мест %d — похоже, горят все рамки сразу", n)
	}
}

// Проверка обязана запускаться из любой панели — это была главная жалоба.
func TestCheckStartsFromEveryPane(t *testing.T) {
	for _, p := range paneOrder {
		t.Run(p.String(), func(t *testing.T) {
			m, _ := newTestModel(t)
			m = openFirstLesson(t, m)
			m = update(t, m, key(rune('0'+int(p)+1))).(Model)
			if m.focus != p {
				t.Fatalf("не переключились на %v", p)
			}

			updated, cmd := m.Update(key('r'))
			m = updated.(Model)
			if !m.running || cmd == nil {
				t.Errorf("r не запустил проверку из панели %v", p)
			}
		})
	}
}

// ---------- проверка ----------

func TestCheckMarksPassAndFail(t *testing.T) {
	m, ws := newTestModel(t)
	m = openFirstLesson(t, m)

	// 1. Заготовка — тесты красные.
	m = runCheckOnscreen(t, m)
	got := content(t, m)
	if !strings.Contains(got, "падают") && !strings.Contains(got, "красные") {
		t.Errorf("несделанный урок не выглядит красным\n%s", got)
	}
	if m.store.Get("01-basics").Done {
		t.Error("урок помечен сданным, хотя проверки красные")
	}

	// 2. Эталонное решение — зелёное, прогресс записан.
	sol, err := os.ReadFile(filepath.Join("..", "..", "testdata", "solutions", "01-basics", "tasks.go"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "lessons", "01-basics", "tasks.go"), sol, 0o644); err != nil {
		t.Fatal(err)
	}

	m = runCheckOnscreen(t, m)
	if !strings.Contains(content(t, m), "Все зелёные") {
		t.Errorf("сделанный урок не выглядит зелёным\n%s", content(t, m))
	}
	rec := m.store.Get("01-basics")
	if !rec.Done {
		t.Error("прогресс не записан")
	}
	if rec.Passed == 0 {
		t.Errorf("сдано, но Passed = %d — счётчик проверок не работает", rec.Passed)
	}
	if _, err := os.Stat(filepath.Join(ws, ".progress.json")); err != nil {
		t.Errorf("файл прогресса не появился: %v", err)
	}
}

// Счётчик «сколько проверок прошло» — ради него и нужен go test -json.
func TestCheckCountsTests(t *testing.T) {
	m, _ := newTestModel(t)
	m = openFirstLesson(t, m)
	m = runCheckOnscreen(t, m)

	rep := m.report
	if rep == nil {
		t.Fatal("отчёта нет")
	}
	if rep.Passed+rep.Failed == 0 {
		t.Fatalf("парсер go test -json не нашёл ни одного теста:\n%s", dumpSteps(rep))
	}
	if rep.Failed == 0 {
		t.Error("на заготовке хоть один тест да должен падать")
	}
	if !strings.Contains(content(t, m), "из") {
		t.Error("в интерфейсе нет счётчика вида «N из M»")
	}
}

// Клавиша g ведёт редактор на конкретную строку — без координат она бесполезна.
func TestFirstProblemHasCoordinates(t *testing.T) {
	m, _ := newTestModel(t)
	m = openFirstLesson(t, m)
	m = runCheckOnscreen(t, m)

	p := m.firstProblem()
	if p == nil {
		t.Fatalf("после красной проверки нет ни одной координаты:\n%s", dumpSteps(m.report))
	}
	if p.File == "" || p.Line == 0 {
		t.Errorf("координаты неполные: %+v", p)
	}
	if !strings.HasSuffix(p.File, ".go") {
		t.Errorf("файл ошибки выглядит странно: %q", p.File)
	}
	if !strings.Contains(content(t, m), "g — открыть") {
		t.Error("подсказки про g на экране нет")
	}
}

func TestGotoWithoutReportIsHarmless(t *testing.T) {
	m, _ := newTestModel(t)
	m = openFirstLesson(t, m)

	updated, cmd := m.Update(key('g'))
	m = updated.(Model)
	if cmd != nil {
		t.Error("без проверки g зачем-то запустила редактор")
	}
	if !strings.Contains(m.notice, "не на что указывать") {
		t.Errorf("непонятный ответ: %q", m.notice)
	}
}

// ---------- редактор ----------

func TestEditorCommandFallsBackToEnv(t *testing.T) {
	t.Setenv("EDITOR", "myeditor -w")
	t.Setenv("VISUAL", "")
	t.Setenv("PATH", t.TempDir()) // заведомо пустой: code там не найдётся

	bin, args, err := editorCommand(nil, "/tmp/x")
	if err != nil {
		t.Fatalf("не нашли редактор вообще: %v", err)
	}
	if bin != "myeditor" {
		t.Errorf("взяли %q, ждали myeditor", bin)
	}
	if args[len(args)-1] != "/tmp/x" {
		t.Errorf("путь не в конце списка аргументов: %v", args)
	}
}

func TestEditorCommandWithoutAnything(t *testing.T) {
	t.Setenv("EDITOR", "")
	t.Setenv("VISUAL", "")
	t.Setenv("PATH", t.TempDir())

	if _, _, err := editorCommand(nil, "/tmp/x"); err == nil {
		t.Error("без code и без EDITOR надо честно сообщать об этом, а не молча запускать пустоту")
	}
}

func TestEditorCommandPassesGoto(t *testing.T) {
	t.Setenv("EDITOR", "ed")
	t.Setenv("VISUAL", "")
	t.Setenv("PATH", t.TempDir())

	_, args, err := editorCommand([]string{"--goto"}, "/tmp/x/tasks.go")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, a := range args {
		if a == "--goto" {
			found = true
		}
	}
	if !found {
		t.Errorf("флаг --goto потерялся: %v", args)
	}
}

// ---------- теория ----------

func TestTheoryIsRendered(t *testing.T) {
	m, _ := newTestModel(t)
	m = openFirstLesson(t, m)

	if !strings.Contains(m.theoryVP.GetContent(), "Переменные") {
		t.Error("теория не попала в панель")
	}
	// Сырой markdown с решётками заголовков наружу выходить не должен.
	if strings.Contains(m.theoryVP.GetContent(), "# Переменные") {
		t.Error("показываем markdown вместо отрендеренного текста")
	}
}

// Длинные строки вывода должны переноситься, а не обрезаться — ровно тот баг,
// из-за которого «хотим 5» превращалось в «хоти».
func TestLongOutputIsWrappedNotCut(t *testing.T) {
	m, _ := newTestModel(t)
	m = openFirstLesson(t, m)
	m = runCheckOnscreen(t, m)

	// Смотрим на содержимое панели, а не на кадр: в кадр попадает только
	// видимое окно, и длинный вывод уехал бы за его край без всякой обрезки.
	full := ansi.Strip(m.resultVP.GetContent())

	// Перенос вставляет перевод строки, поэтому ищем по тексту со склеенными
	// строками: так видно, что ничего не потерялось.
	joined := strings.Join(strings.Fields(full), " ")
	for _, want := range []string{`хотим "Привет, Аня!"`, "TestWordCount"} {
		if !strings.Contains(joined, want) {
			t.Errorf("в выводе нет %q — похоже, строку обрезало\n----\n%s", want, full)
		}
	}

	// Ни одна строка не шире панели: иначе viewport дорежет её сам.
	for _, line := range strings.Split(full, "\n") {
		if n := lipgloss.Width(line); n > m.resultVP.Width() {
			t.Errorf("строка %d колонок при панели %d: %q", n, m.resultVP.Width(), line)
		}
	}
}

// accentSequence — ANSI-последовательность цвета акцента: по ней и считаем,
// сколько рамок подсвечено.
var accentSequence = lipgloss.NewStyle().Foreground(cAccent).Render("X")

// dumpSteps — содержимое отчёта для сообщений об ошибках теста.
func dumpSteps(r *runner.Report) string {
	if r == nil {
		return "<отчёта нет>"
	}
	var b strings.Builder
	for _, s := range r.Steps {
		fmt.Fprintf(&b, "%s ok=%v\n%s\n\n", s.Name, s.OK, s.Output)
	}
	for _, tr := range r.Tests {
		fmt.Fprintf(&b, "тест %s: %s\n", tr.Name, tr.Status)
	}
	return b.String()
}
