package base62

import "strings"
import "fmt"

const DefaultAlphabet = "abcdefghijklmnopqrstuvwxyz0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"
const ShuffledAlphabet = "krKL5Z0Uz9tiXh3lNsq1MFVmPcdIeoyB28vWGupQS7H6wOYDnJbfEgTxRAa4Cj"
const _base = 62

type Encoder struct {
	alphabet string
}

func NewEncoder(alphabet string) (*Encoder, error) {
	if len(alphabet) != _base {
		return nil, fmt.Errorf("Base62 alphabet must have 62 characters")
	}
	return &Encoder{alphabet: alphabet}, nil
}

func NewEncoderOrPanic(alphabet string) *Encoder {
	if len(alphabet) != _base {
		panic(fmt.Errorf("Base62 alphabet must have 62 characters"))
	}
	return &Encoder{alphabet: alphabet}
}

// base62Encode encodes a number to a base62 string representation.
func (e *Encoder) Encode(num int64) string {
	if num == 0 {
		return string([]uint8{ e.alphabet[0] })
	}

	arr := []uint8{}

	for num > 0 {
		rem := num % _base
		num = num / _base
		arr = append(arr, e.alphabet[rem])
	}

	for i, j := 0, len(arr)-1; i < j; i, j = i+1, j-1 {
		arr[i], arr[j] = arr[j], arr[i]
	}

	return string(arr)
}

func (e *Encoder) Decode(num string) (val int64) {
	val = 0

	for i := 0; i < len(num); i++ {
		c := strings.IndexByte(e.alphabet, num[i])
		val = (val * _base) + int64(c)
	}

	return
}
