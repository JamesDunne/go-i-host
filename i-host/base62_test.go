package main

import "testing"

func Test_base62Decode(t *testing.T) {
	t.Log(base62Encode(10001))

	t.Log(base62Decode("K70"))
	t.Log(base62Decode("K71"))

	t.Log(base62Decode(base62Encode(10001)))
}
