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
	// Таймаут 5s на шаг; если висит дольше ~20s — группа процессов не убилась.
	if took > 25*time.Second {
		t.Errorf("убийство затянулось: %s — похоже, группа процессов осталась жива", took)
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
