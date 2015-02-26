package main

import "testing"

func Test_api(t *testing.T) {
	api, err := NewAPI()
	if err != nil {
		panic(err)
	}

	defer api.Close()
}
