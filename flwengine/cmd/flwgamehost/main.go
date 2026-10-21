package main

import (
	"os"

	"flwengine/internal/gamehost"
)

func main() {
	os.Exit(gamehost.CliMain(os.Args[1:]))
}
