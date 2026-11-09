// Команда go-lessons — интерактивный тренажёр языка Go в терминале.
//
//	go-lessons              интерфейс
//	go-lessons list         список уроков
//	go-lessons check <id>   проверить урок без интерфейса (удобно в скриптах)
package main

import (
	"context"
	"embed"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/shushanskii/go-lessons/internal/lesson"
	"github.com/shushanskii/go-lessons/internal/progress"
	"github.com/shushanskii/go-lessons/internal/runner"
	"github.com/shushanskii/go-lessons/internal/ui"
)

// Уроки вшиты в бинарник: скачал один файл — и он работает.
//
// Каталог называется testdata не от лени: go build ./... пропускает папки с
// таким именем. Иначе заготовки уроков и их тесты пытались бы собраться вместе
// с приложением.
//
//go:embed all:testdata/lessons
var lessonsFS embed.FS

func main() {
	fs := flag.NewFlagSet("go-lessons", flag.ExitOnError)
	wsFlag := fs.String("workspace", "",
		"папка с кодом уроков (по умолчанию $GO_LESSONS_WORKSPACE или ~/go-lessons-practice)")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, usage)
		fs.PrintDefaults()
	}
	_ = fs.Parse(os.Args[1:])

	workspace, err := resolveWorkspace(*wsFlag)
	if err != nil {
		die(err)
	}

	reg, err := lesson.Load(mustSub(lessonsFS, "testdata/lessons"))
	if err != nil {
		die(fmt.Errorf("не смог прочитать уроки: %w", err))
	}
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		die(fmt.Errorf("не создать рабочую папку %s: %w", workspace, err))
	}
	if err := ensureModule(workspace); err != nil {
		die(err)
	}

	store, err := progress.Open(filepath.Join(workspace, ".progress.json"))
	if err != nil {
		// Повреждённый прогресс не повод не запускаться — скажем и живём дальше.
		fmt.Fprintln(os.Stderr, "предупреждение:", err)
	}

	args := fs.Args()
	switch {
	case len(args) > 0 && args[0] == "list":
		cmdList(reg, store, workspace)
	case len(args) > 1 && args[0] == "check":
		cmdCheck(reg, store, workspace, args[1])
	case len(args) > 0 && args[0] == "help":
		fmt.Print(usage)
	default:
		if err := runTUI(reg, store, workspace); err != nil {
			die(err)
		}
	}
}

const usage = `go-lessons — интерактивный тренажёр Go в терминале

Запуск без аргументов открывает интерфейс.

Команды:
  list              список уроков и прогресс
  check <id>        проверить урок без интерфейса, код возврата 0 если сдан
  help              этот текст

Флаги:
`

// ---------- команды ----------

func cmdList(reg *lesson.Registry, store *progress.Store, workspace string) {
	fmt.Printf("Рабочая папка: %s\n\n", workspace)
	for i, l := range reg.All {
		rec := store.Get(l.ID)
		status := "не начат"
		switch {
		case rec.Done:
			status = "сдан"
		case rec.Attempts > 0:
			status = fmt.Sprintf("попыток %d", rec.Attempts)
		}
		fmt.Printf("%2d  %-12s %-28s %s\n", i+1, l.ID, l.Title, status)
	}
	fmt.Printf("\nСдано %d из %d\n", store.DoneCount(), len(reg.All))
}

func cmdCheck(reg *lesson.Registry, store *progress.Store, workspace, id string) {
	l := find(reg, id)
	if l == nil {
		die(fmt.Errorf("нет урока %q — список покажет go-lessons list", id))
	}
	if _, err := l.Unpack(workspace); err != nil {
		die(err)
	}

	rep := runner.Check(context.Background(), runner.Options{
		Dir:     l.Dir(workspace),
		AlsoRun: l.Run,
		Race:    l.Race,
	})
	_ = store.Pass(l.ID, rep.OK)

	if rep.Fatal != "" {
		fmt.Fprintln(os.Stderr, rep.Fatal)
		os.Exit(2)
	}
	for _, s := range rep.Steps {
		mark := "FAIL"
		if s.OK {
			mark = "ok  "
		}
		fmt.Printf("%s  %-8s %6s\n", mark, s.Name, s.Elapsed.Round(time.Millisecond))
		if !s.OK && strings.TrimSpace(s.Output) != "" {
			fmt.Println(indent(s.Output, "      "))
		}
	}
	fmt.Printf("\n%s   всего %s\n", verdict(rep), rep.Elapsed.Round(10*time.Millisecond))
	if !rep.OK {
		os.Exit(1)
	}
}

func runTUI(reg *lesson.Registry, store *progress.Store, workspace string) error {
	p := tea.NewProgram(ui.New(reg, store, workspace))
	_, err := p.Run()
	return err
}

// ---------- вспомогательное ----------

// ensureModule делает из рабочей папки обычный Go-модуль.
//
// Без него go test отвечает «go.mod file not found», а с ним студент может
// выйти из тренажёра и проверить себя руками: go test ./01-basics/
//
// go 1.22 — не от жадности: с этой версии переменная цикла создаётся заново
// на каждой итерации, и урок про горутины на это опирается.
func ensureModule(workspace string) error {
	path := filepath.Join(workspace, "go.mod")
	if _, err := os.Stat(path); err == nil {
		return nil // студент мог сам поправить зависимости — не трогаем
	}
	body := "module golessons\n\ngo 1.22\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return fmt.Errorf("не создать %s: %w", path, err)
	}
	return nil
}

func resolveWorkspace(flagVal string) (string, error) {
	if flagVal != "" {
		return filepath.Abs(flagVal)
	}
	if env := os.Getenv("GO_LESSONS_WORKSPACE"); env != "" {
		return filepath.Abs(env)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("не понял, где домашний каталог: %w", err)
	}
	return filepath.Join(home, "go-lessons-practice"), nil
}

// find ищет урок по имени каталога или по номеру: check 2 == check 02-slices.
func find(reg *lesson.Registry, id string) *lesson.Lesson {
	for _, l := range reg.All {
		if l.ID == id {
			return l
		}
	}
	for i, l := range reg.All {
		if fmt.Sprint(i+1) == id || fmt.Sprintf("%02d", i+1) == id {
			return l
		}
	}
	return nil
}

func verdict(r *runner.Report) string {
	if r.OK {
		return "СДАН"
	}
	if r.TimedOut {
		return "ПРЕВЫШЕНО ВРЕМЯ"
	}
	return "НЕ СДАН"
}

func indent(s, pad string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		lines[i] = pad + l
	}
	return strings.Join(lines, "\n")
}

func mustSub(fsys embed.FS, dir string) fs.FS {
	sub, err := fs.Sub(fsys, dir)
	if err != nil {
		die(fmt.Errorf("внутри бинарника нет каталога %s: %w", dir, err))
	}
	return sub
}

func die(err error) {
	if errors.Is(err, context.Canceled) {
		return
	}
	fmt.Fprintln(os.Stderr, "go-lessons:", err)
	os.Exit(1)
}
