package runner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func module(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write(t, dir, "go.mod", "module probe\n\ngo 1.22\n")
	return dir
}

func TestPassingTest(t *testing.T) {
	dir := module(t)
	write(t, dir, "a.go", "package probe\n")
	write(t, dir, "a_test.go", "package probe\n\nimport \"testing\"\n\nfunc TestOk(t *testing.T) {}\n")

	rep := Check(context.Background(), Options{Dir: dir})
	if rep.Fatal != "" {
		t.Fatalf("проверка не состоялась: %s", rep.Fatal)
	}
	if !rep.OK {
		t.Errorf("хотели зелёное, получили\n%s", dump(rep))
	}
	if len(rep.Steps) != 2 {
		t.Errorf("шагов %d, хотели 2", len(rep.Steps))
	}
}

func TestFailingTestStopsAfterTests(t *testing.T) {
	dir := module(t)
	write(t, dir, "a.go", "package probe\n\nfunc Bad( {\n")

	rep := Check(context.Background(), Options{Dir: dir, AlsoRun: true})
	if rep.OK {
		t.Error("код с синтаксической ошибкой прошёл проверку")
	}
	if len(rep.Steps) != 1 {
		t.Errorf("остановились не на первом шаге: %d шагов, вывод %q",
			len(rep.Steps), rep.Steps[len(rep.Steps)-1].Output)
	}
	if !strings.Contains(rep.Steps[0].Output, "expected") &&
		!strings.Contains(rep.Steps[0].Output, "syntax") {
		t.Errorf("в выводе нет внятной ошибки парсера: %q", rep.Steps[0].Output)
	}
}

// Урок про горутины держится на этом тесте: без убийства группы процессов
// зависший код студента остался бы висеть после проверки.
func TestInfiniteLoopIsKilled(t *testing.T) {
	dir := module(t)
	write(t, dir, "a.go", "package probe\n\nfunc Spin() int {\n\tfor {\n\t}\n}\n")
	write(t, dir, "a_test.go", "package probe\n\nimport \"testing\"\n\n"+
		"func TestSpin(t *testing.T) {\n\tif Spin() == 1 {\n\t\tt.Log(\"не бывает\")\n\t}\n}\n")

	start := time.Now()
	rep := Check(context.Background(), Options{Dir: dir, Timeout: 5 * time.Second})
	took := time.Since(start)

	if rep.OK {
		t.Error("бесконечный цикл прошёл проверку")
	}
	if !rep.TimedOut {
		t.Errorf("не помечено как превышение времени: %s", dump(rep))
	}
	// Таймаут 5s на шаг; если висит дольше ~25s — группа процессов не убилась.
	if took > 25*time.Second {
		t.Errorf("убийство затянулось: %s — похоже, группа процессов осталась жива", took)
	}
}

// Координаты нужны для перехода в редактор по клавише g.
func TestProblemsAreLocated(t *testing.T) {
	dir := module(t)
	write(t, dir, "a.go", "package probe\n\nfunc Bad( {\n")

	rep := Check(context.Background(), Options{Dir: dir})
	if rep.OK {
		t.Fatal("код с синтаксической ошибкой прошёл проверку")
	}
	if len(rep.Steps) == 0 || len(rep.Steps[0].Problems) == 0 {
		t.Fatalf("нет координат ошибки:\n%s", dump(rep))
	}

	p := rep.Steps[0].Problems[0]
	if p.File != "a.go" {
		t.Errorf("файл %q, ждали a.go", p.File)
	}
	if p.Line == 0 {
		t.Errorf("строки нет: %+v", p)
	}
	if !strings.Contains(p.String(), "a.go:3") {
		t.Errorf("вид не тот: %q", p.String())
	}
}

// Счётчик тестов — ради него всё и затевалось: «не сдан» бесполезно,
// а «падает 2 из 5» говорит студенту, что уже хорошо.
func TestCountsPerTest(t *testing.T) {
	dir := module(t)
	write(t, dir, "a.go", "package probe\n\nfunc Two() int { return 2 }\n")
	write(t, dir, "a_test.go", "package probe\n\nimport \"testing\"\n\n"+
		"func TestGood(t *testing.T) {\n\tif Two() != 2 { t.Fatal(\"нет\") }\n}\n\n"+
		"func TestBad(t *testing.T) {\n\tif Two() != 3 {\n\t\tt.Errorf(\"Two() = %d, хотим 3\", Two())\n\t}\n}\n")

	rep := Check(context.Background(), Options{Dir: dir})
	if rep.OK {
		t.Error("падающий тест прошёл как успешный")
	}
	if rep.Passed != 1 || rep.Failed != 1 {
		t.Errorf("счётчик %+v/%+v, ждали 1/1:\n%s", rep.Passed, rep.Failed, dump(rep))
	}
	if len(rep.Tests) != 2 {
		t.Errorf("тестов найдено %d, ждали 2", len(rep.Tests))
	}

	var where *Problem
	for _, tr := range rep.Tests {
		if tr.Name == "TestBad" {
			where = tr.Where
		}
	}
	if where == nil || where.File != "a_test.go" || where.Line == 0 {
		t.Errorf("у падающего теста нет координат: %+v", where)
	}
}

// Паника вне тестов (дедлок, незакрытый канал) обязана дойти до студента,
// а не превратиться в «ни одного теста».
func TestPanicIsReported(t *testing.T) {
	dir := module(t)
	write(t, dir, "a.go", "package probe\n\nfunc Wait() int {\n\tc := make(chan int)\n\treturn <-c\n}\n")
	write(t, dir, "a_test.go", "package probe\n\nimport \"testing\"\n\n"+
		"func TestWait(t *testing.T) {\n\t_ = Wait()\n}\n")

	rep := Check(context.Background(), Options{Dir: dir, Timeout: 40 * time.Second})
	if rep.OK {
		t.Error("вечное ожидание канала прошло проверку")
	}
	if len(rep.Steps) < 2 {
		t.Fatalf("до тестов не дошло:\n%s", dump(rep))
	}
	tests := rep.Steps[1]
	if tests.OK {
		t.Error("шаг тестов зелёный при зависшем тесте")
	}
	if !strings.Contains(tests.Output, "deadlock") && !strings.Contains(tests.Output, "panic") {
		t.Errorf("в выводе нет ни deadlock, ни panic:\n%s", tests.Output)
	}
}

func TestMissingGoIsFatal(t *testing.T) {
	dir := module(t)
	rep := Check(context.Background(), Options{Dir: filepath.Join(dir, "нет-такой-папки")})
	if rep.Fatal == "" {
		t.Error("для несуществующей папки ожидали Fatal, получили молчание")
	}
}

func TestGoFound(t *testing.T) {
	if findGo() == "" {
		t.Skip("в этой среде нет компилятора go")
	}
}

func dump(r *Report) string {
	var b strings.Builder
	for _, s := range r.Steps {
		b.WriteString(s.Name + ": " + s.Output + "\n")
	}
	return b.String()
}
