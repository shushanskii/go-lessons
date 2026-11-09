// Package runner проверяет код студента: go vet, go test и, если нужно, go run.
package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Шаг проверки — одна команда с её выводом.
type Step struct {
	Name    string // что показали студенту: «vet», «тесты»
	Cmd     string // что реально запустили
	Output  string
	OK      bool
	Elapsed time.Duration
}

// Report — итог одной проверки.
type Report struct {
	Steps    []Step
	OK       bool
	TimedOut bool
	Elapsed  time.Duration
	// Fatal — проверка не состоялась: нет компилятора, не туда записались.
	Fatal string
}

// Options — настройки прогона.
type Options struct {
	// Dir — папка урока внутри рабочей директории.
	Dir string
	// AlsoRun — дополнительно выполнить go run и показать вывод.
	AlsoRun bool
	// Race — добавить -race к тестам.
	Race bool
	// Timeout — сколько ждать одну команду. Ноль означает «сам подберу».
	Timeout time.Duration
}

// stepSpec — один шаг проверки: что показать и что запустить.
type stepSpec struct {
	name string
	args []string
}

const defaultTimeout = 30 * time.Second

// Check последовательно прогоняет vet, тесты и (опционально) запуск.
//
// Останавливается на первом провале: показывать вывод тестов, когда код даже
// не парсится, бессмысленно — студенту нужен первый текст ошибки.
func Check(ctx context.Context, o Options) *Report {
	timeout := o.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
		if o.Race {
			// Сборка с детектором гонок заметно дольше обычной.
			timeout = 90 * time.Second
		}
	}

	goBin := findGo()
	if goBin == "" {
		return &Report{Fatal: "не нашёл компилятор go. Установи его — brew install go — " +
			"или запусти приложение из оболочки, где go есть в PATH"}
	}
	if _, err := os.Stat(o.Dir); err != nil {
		return &Report{Fatal: fmt.Sprintf("нет папки урока: %s", o.Dir)}
	}

	rep := &Report{OK: true}
	start := time.Now()

	tests := stepSpec{"тесты", []string{"test", "-count=1", "-timeout", "15s", "."}}
	if o.Race {
		tests = stepSpec{"тесты + гонки", []string{"test", "-race", "-count=1", "-timeout", "60s", "."}}
	}
	steps := []stepSpec{{"разбор", []string{"vet", "."}}, tests}
	if o.AlsoRun {
		steps = append(steps, stepSpec{"запуск", []string{"run", "."}})
	}

	for _, s := range steps {
		t0 := time.Now()
		out, err, timedOut := runCmd(ctx, goBin, s.args, o.Dir, timeout)
		step := Step{
			Name:    s.name,
			Cmd:     "go " + strings.Join(s.args, " "),
			Output:  strings.TrimRight(out, "\n"),
			OK:      err == nil,
			Elapsed: time.Since(t0),
		}
		rep.Steps = append(rep.Steps, step)

		if timedOut {
			rep.TimedOut = true
		}
		if err != nil || timedOut {
			rep.OK = false
			break
		}
	}

	rep.Elapsed = time.Since(start)
	return rep
}

// runCmd запускает одну команду и возвращает её объединённый вывод.
//
// Процесс уходит в собственную группу (see setProcGroup), потому что
// бесконечный цикл в коде студента убивается только вместе со всей группой:
// одного родителя недостаточно, а go test сам поднимает тестовый бинарь.
func runCmd(ctx context.Context, goBin string, args []string, dir string, timeout time.Duration) (string, error, bool) {
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(cctx, goBin, args...)
	cmd.Dir = dir
	cmd.Env = env()
	setProcGroup(cmd)
	// Если процесс не среагировал на отмену контекста, убиваем группу и
	// перестаём ждать через секунду.
	cmd.Cancel = func() error { return killProcGroup(cmd) }
	cmd.WaitDelay = 2 * time.Second

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	err := cmd.Run()
	timedOut := errors.Is(cctx.Err(), context.DeadlineExceeded)

	out := buf.String()
	if timedOut {
		out += fmt.Sprintf("\n[проверка прервана: прошло %s, код не завершился — похоже на бесконечный цикл]", timeout)
		return out, err, true
	}
	return out, err, false
}

// env — окружение запуска. Флаги пользователя намеренно сброшены: GOFLAGS из
// ~/.config может незаметно добавить -race или -mod=vendor и сломать проверку.
func env() []string {
	e := os.Environ()
	out := make([]string, 0, len(e)+2)
	for _, kv := range e {
		if strings.HasPrefix(kv, "GOFLAGS=") || strings.HasPrefix(kv, "GO111MODULE=") {
			continue
		}
		out = append(out, kv)
	}
	return append(out, "GO111MODULE=on", "GOFLAGS=")
}

// findGo ищет компилятор: сначала в PATH, потом в типовых местах установки.
// Из GUI-запуска PATH бывает урезан, и тогда go есть, но не находится.
func findGo() string {
	if p, err := exec.LookPath("go"); err == nil {
		return p
	}
	home := os.Getenv("HOME")
	candidates := []string{
		"/opt/homebrew/bin/go", // Apple Silicon, Homebrew
		"/usr/local/bin/go",    // Intel, Homebrew
		"/usr/local/go/bin/go", // официальный установщик
	}
	if home != "" {
		candidates = append(candidates, filepath.Join(home, "go/bin/go"))
	}
	for _, c := range candidates {
		if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
			return c
		}
	}
	return ""
}
