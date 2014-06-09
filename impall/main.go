package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"strconv"
)

import "github.com/JamesDunne/i-host/base62"

var b62 = base62.NewEncoderOrPanic(base62.ShuffledAlphabet)

func panicIf(err error) {
	if err == nil {
		return
	}
	panic(err)
}

func main() {
	// Open the directory to read its contents:
	f, err := os.Open(`/srv/bittwiddlers.org/i/links`)
	if err != nil {
		panic(err)
	}
	defer f.Close()

	// Read the directory entries:
	fis, err := f.Readdir(0)
	panicIf(err)

	i2 := make(map[int64]bool, 350)
	imp, err := ioutil.ReadFile("i2")
	panicIf(err)

	br := bytes.NewReader(imp)
	s := bufio.NewScanner(br)
	for s.Scan() {
		line := s.Text()
		id, err := strconv.ParseInt(line, 10, 64)
		if err != nil {
			continue
		}
		//fmt.Printf("%d\n", id)
		i2[id] = true
	}

	sql, err := os.Create("import.sql")
	panicIf(err)
	defer sql.Close()
	cp, err := os.Create("copy.sh")
	panicIf(err)
	defer cp.Close()

	for _, fi := range fis {
		name := fi.Name()
		base62id := name[0 : len(name)-len(path.Ext(name))]

		id := b62.Decode(base62id) - 10000
		if _, has := i2[id]; has {
			fmt.Printf("Skip %d!\n", id)
			continue
		}

		fmt.Printf("%d\n", id)
		fmt.Fprintf(sql, "insert into Image (ID,Title,Kind) values (%[1]d,'%[1]d','gif');\n", id)
		fmt.Fprintf(cp, "cp -L /srv/bittwiddlers.org/i/links/%s /srv/bittwiddlers.org/i2/store/%d.gif\n", name, id)
	}
}
