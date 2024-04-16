package main

import (
	"fmt"
	"strconv"
	"testing"
)

func TestXxx(t *testing.T) {
	pend := "0x1ae"
	pi, _ := strconv.ParseUint(pend[2:], 16, 64)
	fmt.Println(pi)
}
