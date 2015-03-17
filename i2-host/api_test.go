package main

import (
	"log"
	"testing"
)
import (
	"strings"
)

func Test_api(t *testing.T) {
	api, err := NewAPI()
	if err != nil {
		panic(err)
	}

	defer api.Close()
}

// Set all Keywords for images:
func TestSetKeywords(t *testing.T) {
	api, err := NewAPI()
	if err != nil {
		panic(err)
	}
	defer api.Close()

	imgs, err := api.GetAll(ImagesOrderByIDASC)
	for _, img := range imgs {
		if img.Keywords != "" {
			continue
		}

		keywords := splitToWords(strings.ToLower(img.Title))
		//log.Printf("%+v\n", keywords)

		img.Keywords = strings.Join(keywords, " ")

		err = api.Update(&img)
		if err != nil {
			log.Println(err)
		}
	}
}
