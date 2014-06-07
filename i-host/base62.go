package main

import "strings"

// Shuffled            "abcdefghijklmnopqrstuvwxyz0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"
const base62Alphabet = "krKL5Z0Uz9tiXh3lNsq1MFVmPcdIeoyB28vWGupQS7H6wOYDnJbfEgTxRAa4Cj"
const _base = int64(len(base62Alphabet))

// base62Encode encodes a number to a base62 string representation.
func base62Encode(num int64) string {
	if num == 0 {
		return "0"
	}

	arr := []uint8{}

	for num > 0 {
		rem := num % _base
		num = num / _base
		arr = append(arr, base62Alphabet[rem])
	}

	for i, j := 0, len(arr)-1; i < j; i, j = i+1, j-1 {
		arr[i], arr[j] = arr[j], arr[i]
	}

	return string(arr)
}

func base62Decode(num string) (val int64) {
	val = 0

	for i := 0; i < len(num); i++ {
		c := strings.IndexByte(base62Alphabet, num[i])
		val = (val * _base) + int64(c)
	}

	return
}
