package main

import (
	"encoding/json"
	"fmt"
	"os"

	"flwengine/internal/plat"
)

func main() {
	info, err := plat.Detect()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	out, _ := json.MarshalIndent(info, "", "  ")
	fmt.Println(string(out))
}
