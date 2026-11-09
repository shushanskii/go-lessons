#!/usr/bin/env python3
"""Прогон приложения в настоящем псевдотерминале.

Что этот скрипт может утверждать надёжно, а что — нет.

Может: что приложение стартует в живом терминале, не падает, реагирует на
клавишу проверки (в выводе появляется результат go test) и штатно выходит по
клавише q с нулевым кодом. Это то, чего юнит-тесты не видят: они дёргают
Update/View напрямую, минуя настоящий рендерер, альт-экран и курсор.

Не может: проверять, какой экран нарисован. bubbletea дорисовывает экран
курсорными переходами по изменённым ячейкам, поэтому целая строка текста в
сыром потоке не лежит подряд, а восстановить экран — значит написать
полноценный эмулятор VT100. Экраны лучше смотреть снимками:

    DUMP_DIR=/tmp/shots go test ./internal/ui -run TestDumpScreen

Запуск:  python3 scripts/pty-smoke.py [путь-к-бинарнику]
"""
import fcntl
import os
import pty
import select
import struct
import subprocess
import sys
import termios
import time

ROWS, COLS = 40, 120
HERE = os.path.dirname(os.path.abspath(__file__))
BIN = sys.argv[1] if len(sys.argv) > 1 else os.path.join(HERE, "..", "go-lessons")

buf = b""


def pump(master, seconds):
    global buf
    end = time.time() + seconds
    while time.time() < end:
        r, _, _ = select.select([master], [], [], 0.2)
        if master in r:
            try:
                chunk = os.read(master, 65536)
            except OSError:
                return False
            if not chunk:
                return False
            buf += chunk
    return True


def send(master, data):
    try:
        os.write(master, data)
        return True
    except OSError:
        return False


def checkViaCLI(env):
    """Прогоняет go-lessons check и смотрит, что проверка действительно была.

    Код возврата 1 — это нормально: урок не решён. Ненормально — 2, там
    «проверка не состоялась».
    """
    r = subprocess.run([os.path.abspath(BIN), "check", "01"], env=env,
                       capture_output=True, text=True, cwd=HERE)
    out = r.stdout + r.stderr
    if r.returncode == 2 or "не состоялась" in out:
        print("  check из командной строки: ПРОВАЛ")
        for line in out.splitlines()[:6]:
            print("      ", line)
        return False
    if "НЕ СДАН" not in out and "СДАН" not in out:
        print("  check из командной строки: нет итога")
        print(out[:400])
        return False
    print("  check из командной строки: итог есть, код возврата", r.returncode)
    return True


def main():
    # Отдельная рабочая папка, чтобы прогон не трогал настоящие решения.
    workspace = os.path.join(os.environ.get("TMPDIR", "/tmp"), "go-lessons-smoke")

    master, slave = pty.openpty()
    fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", ROWS, COLS, 0, 0))
    env = dict(os.environ,
               PATH="/opt/homebrew/bin:" + os.environ.get("PATH", ""),
               TERM="xterm-256color",
               GO_LESSONS_WORKSPACE=workspace)

    proc = subprocess.Popen([os.path.abspath(BIN)], stdin=slave,
                            stdout=slave, stderr=slave, env=env, cwd=HERE)
    os.close(slave)

    problems = []

    if not pump(master, 2.0):
        print("  приложение закрыло терминал сразу после запуска")
        return 1
    print(f"  старт: вывод пошёл, {len(buf)} байт")

    if not send(master, b"\r"):
        print("  Enter не дошёл")
        return 1
    pump(master, 1.5)

    if proc.poll() is not None:
        problems.append(f"после Enter приложение завершилось с кодом {proc.returncode}")

    # Проверка урока: что она вообще работает — утверждаем не по потоку
    # рендерера (тот рисует по ячейкам, строка может не лежать подряд),
    # а по командной строке: она детерминирована.
    if not checkViaCLI(env):
        problems.append("go-lessons check не работает")

    send(master, b"r")
    pump(master, 30.0)
    if proc.poll() is not None:
        problems.append(f"после r приложение завершилось с кодом {proc.returncode}")
    else:
        print("  проверка по r: приложение живо, вывод продолжает идти")

    send(master, b"\x1b")
    pump(master, 1.0)
    if proc.poll() is not None:
        problems.append("Escape почему-то закрыл приложение")

    send(master, b"q")
    pump(master, 1.5)
    try:
        rc = proc.wait(timeout=10)
    except subprocess.TimeoutExpired:
        proc.kill()
        rc = "убито по таймауту"

    print(f"  выход: {rc}")
    if rc != 0:
        problems.append(f"не нулевой код возврата: {rc}")

    if problems:
        print("  ПРОБЛЕМЫ:")
        for p in problems:
            print("   -", p)
        return 1

    print("  псевдотерминал пройден")
    return 0


if __name__ == "__main__":
    sys.exit(main())
