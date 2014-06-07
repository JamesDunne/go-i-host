package main

import "testing"
import "github.com/JamesDunne/go-util/base"

func Test_api(t *testing.T) {
	api, err := NewAPI()
	if err != nil {
		panic(err)
	}

	defer api.Close()

	api.NewImage("")
}
