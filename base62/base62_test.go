package base62

import "testing"

func Test_base62Decode(t *testing.T) {
	e, err := NewEncoder(ShuffledAlphabet)
	if err != nil {
		t.Fatal(err)
	}

	t.Log(e.Decode("Kp2") - 10000)
	t.Log(e.Encode(331 + 10000))
}
