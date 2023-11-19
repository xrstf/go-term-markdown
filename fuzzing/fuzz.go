package fuzzing

import markdown "go.xrstf.de/go-term-markdown"

func Fuzz(data []byte) int {
	markdown.Render(string(data), 50, 4)
	return 1
}
