package main

import (
	"fmt"
	"os"
	"strconv"
)
import "github.com/JamesDunne/i-host/base62"

var b62 *base62.Encoder = base62.NewEncoderOrPanic(base62.ShuffledAlphabet)

func main() {
	if len(os.Args) <= 1 {
		fmt.Println("Require integer argument to base62 encode.")
		return
	}
	i, err := strconv.ParseInt(os.Args[1], 10, 64)
	if err != nil {
		fmt.Println(err)
		return
	}
	b := b62.Encode(i)
	fmt.Println(b)
}
