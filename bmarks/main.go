package main

import (
	"bufio"
	"bytes"
	"fmt"
	"html"
	"io/ioutil"
	"sort"
	"strings"
)

import "github.com/JamesDunne/i-host/base62"

var b62 *base62.Encoder = base62.NewEncoderOrPanic(base62.ShuffledAlphabet)

type Image struct {
	ID    int64
	Title string
}

type ByID []Image

func (a ByID) Len() int           { return len(a) }
func (a ByID) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a ByID) Less(i, j int) bool { return a[i].ID < a[j].ID }

func main() {
	f, err := ioutil.ReadFile("gifs.html")
	if err != nil {
		panic(err)
	}

	br := bytes.NewReader(f)
	s := bufio.NewScanner(br)

	imgs := make([]Image, 0, 500)
	for s.Scan() {
		line := s.Text()

		hrefStart := strings.Index(line, "HREF=\"")
		href := line[hrefStart+6:]
		hrefEnd := strings.Index(href, "\"")
		href = href[0:hrefEnd]

		endTag := strings.LastIndex(line, "</A>")
		desc := line[0:endTag]
		descStart := strings.LastIndex(desc, ">")
		desc = html.UnescapeString(desc[descStart+1:])

		const ibiturl = "http://i.bittwiddlers.org/"
		if !strings.HasPrefix(href, ibiturl) {
			continue
		}

		base62ID := html.UnescapeString(href[strings.LastIndex(href, "/")+1:])
		//fmt.Printf("%s: %s\n", base62ID, desc)

		id := b62.Decode(base62ID) - 10000
		if id > 500 {
			continue
		}
		//fmt.Printf("%s: %3d: %s\n", base62ID, id, desc)

		imgs = append(imgs, Image{
			ID:    id,
			Title: desc,
		})
	}

	sort.Sort(ByID(imgs))
	for _, img := range imgs {
		fmt.Printf("insert into Image (ID, Kind, Title) values (%d, 'gif', '%s');\n", img.ID, strings.Replace(img.Title, "'", "''", -1))
	}
}
