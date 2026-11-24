package viewer

func boolWord(v bool, whenTrue, whenFalse string) string {
	if v {
		return whenTrue
	}
	return whenFalse
}

func splitAtRune(s string, pos int) (string, string) {
	if pos <= 0 {
		return "", s
	}
	runes := []rune(s)
	if pos >= len(runes) {
		return s, ""
	}
	return string(runes[:pos]), string(runes[pos:])
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func clampFloat(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
