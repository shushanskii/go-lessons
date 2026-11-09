package basics

import "strings"

func Greeting(name string) string { return "Привет, " + name + "!" }

func Sum(a, b int) int { return a + b }

func WordCount(s string) int { return len(strings.Fields(s)) }
