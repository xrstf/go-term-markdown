package misc

import (
	"fmt"
	"io/ioutil"

	markdown "go.xrstf.de/go-term-markdown"
)

func main() {
	path := "Readme.md"
	source, err := ioutil.ReadFile(path)
	if err != nil {
		panic(err)
	}

	result := markdown.Render(string(source), 80, 6)

	fmt.Println(result)
}
