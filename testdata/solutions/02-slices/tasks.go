package slices

func Evens(in []int) []int {
	var out []int
	for _, v := range in {
		if v%2 == 0 {
			out = append(out, v)
		}
	}
	return out
}

func Dedup(in []string) []string {
	var out []string
	seen := make(map[string]bool)
	for _, w := range in {
		if seen[w] {
			continue
		}
		seen[w] = true
		out = append(out, w)
	}
	return out
}

func Reverse(in []int) {
	for i := 0; i < len(in)/2; i++ {
		last := len(in) - 1 - i
		in[i], in[last] = in[last], in[i]
	}
}
