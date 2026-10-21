package plat

import "time"

func timeAfter(sec int) <-chan time.Time {
	return time.After(time.Duration(sec) * time.Second)
}
