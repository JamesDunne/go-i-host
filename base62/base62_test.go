package base62

import "testing"

func Test_base62Decode(t *testing.T) {
	e, err := NewEncoder(ShuffledAlphabet)
	if err != nil {
		t.Fatal(err)
	}

	t.Log(e.Encode(10001))

	t.Log(e.Decode("K70"))
	t.Log(e.Decode("K71"))

	t.Log(e.Decode(e.Encode(10001)))
}
