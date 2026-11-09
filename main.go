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
  check --all       проверить все уроки
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
	if id == "--all" || id == "all" {
		cmdCheckAll(reg, store, workspace)
		return
	}

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
	_ = store.Pass(l.ID, rep.OK, rep.Passed, rep.Failed)

	if rep.Fatal != "" {
		fmt.Fprintln(os.Stderr, rep.Fatal)
		os.Exit(2)
	}
	for _, s := range rep.Steps {
		mark := "FAIL"
		if s.OK {
			mark = "ok  "
		}
		fmt.Printf("%s  %-16s %6s\n", mark, s.Name, s.Elapsed.Round(time.Millisecond))
		for _, p := range s.Problems {
			fmt.Println("        " + p.String())
		}
		if !s.OK && len(s.Problems) == 0 && strings.TrimSpace(s.Output) != "" {
			fmt.Println(indent(s.Output, "        "))
		}
	}
	for _, tr := range rep.Tests {
		sign := "ok  "
		if tr.Status == "fail" {
			sign = "FAIL"
		} else if tr.Status == "skip" {
			sign = "skip"
		}
		fmt.Printf("%s  %s\n", sign, tr.Name)
	}
	fmt.Printf("\n%s   всего %s\n", verdict(rep), rep.Elapsed.Round(10*time.Millisecond))
	if !rep.OK {
		os.Exit(1)
	}
}

// cmdCheckAll прогоняет все уроки подряд. Заменяет прежний «go test ./... из
// корня»: теперь каждый урок — отдельный модуль, и одной командой их не взять.
func cmdCheckAll(reg *lesson.Registry, store *progress.Store, workspace string) {
	failed := 0
	for _, l := range reg.All {
		if _, err := l.Unpack(workspace); err != nil {
			die(err)
		}
		rep := runner.Check(context.Background(), runner.Options{
			Dir:     l.Dir(workspace),
			AlsoRun: l.Run,
			Race:    l.Race,
		})
		_ = store.Pass(l.ID, rep.OK, rep.Passed, rep.Failed)

		mark := "✓"
		if !rep.OK {
			mark = "✗"
			failed++
		}
		score := ""
		if rep.Passed+rep.Failed > 0 {
			score = fmt.Sprintf("%d/%d", rep.Passed, rep.Passed+rep.Failed)
		}
		fmt.Printf("%s  %-14s %-9s %s\n", mark, l.ID, verdict(rep), score)
		for _, s := range rep.Steps {
			for _, p := range s.Problems {
				fmt.Println("       " + p.String())
			}
			if !s.OK && len(s.Problems) == 0 && strings.TrimSpace(s.Output) != "" {
				fmt.Println(indent(s.Output, "       "))
			}
		}
	}
	fmt.Printf("\nСдано %d из %d\n", len(reg.All)-failed, len(reg.All))
	if failed > 0 {
		os.Exit(1)
	}
}

func runTUI(reg *lesson.Registry, store *progress.Store, workspace string) error {
	p := tea.NewProgram(ui.New(reg, store, workspace))
	_, err := p.Run()
	return err
}

// ---------- вспомогательное ----------

// ensureModule не нужен: каждый урок — самостоятельный модуль, go.mod для него
// создаёт lesson.Unpack. В корне рабочей папки модуля нет, и это правильно:
// уроки не должны зависеть друг от друга.

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
