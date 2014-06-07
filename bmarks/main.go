package main

import "github.com/JamesDunne/i-host/base62"

var b62 *base62.Encoder = base62.NewEncoderOrPanic(base62.ShuffledAlphabet)

func main() {
	b62.Decode("KuM")
}
