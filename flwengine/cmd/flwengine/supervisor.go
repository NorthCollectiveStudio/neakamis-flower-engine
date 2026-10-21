package main

import (
	"fmt"
	"os"

	"flwengine/internal/nuggetos"
)

func runSupervisor() {
	fmt.Println("flower engine supervisor")
	os.Exit(nuggetos.Run())
}
