package main

import (
	"fmt"
	"time"

	"flwengine/internal/gdi"
)

func main() {
	m := gdi.NewManager(false)
	if !m.Start(0) {
		fmt.Println("start: failed")
		return
	}
	defer m.Stop()
	time.Sleep(300 * time.Millisecond)
	r, err := m.Handle("flash", map[string]any{"color": "#ffffff", "alpha": 200, "up": 200, "down": 600})
	fmt.Printf("flash: r=%v err=%v\n", r, err)
	time.Sleep(700 * time.Millisecond)
	r, err = m.Handle("overlay", map[string]any{"color": "#101040", "alpha": 150, "ms": 300})
	fmt.Printf("overlay: r=%v err=%v\n", r, err)
	time.Sleep(600 * time.Millisecond)
	r, err = m.Handle("clear", nil)
	fmt.Printf("clear: r=%v err=%v\n", r, err)
	time.Sleep(300 * time.Millisecond)
	fmt.Println("gdi sequence done")
}
