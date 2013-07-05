package main

import (
	"bufio"
	"log"
	"os"
	"path"
	"strconv"
	"syscall"
)

const (
	links_folder = "/srv/bittwiddlers.org/i/links"
	store_folder = "/srv/bittwiddlers.org/i/store"
)

func startsWith(s, start string) bool {
	if len(s) < len(start) {
		return false
	}
	return s[0:len(start)] == start
}

func removeIfStartsWith(s, start string) string {
	if !startsWith(s, start) {
		return s
	}
	return s[len(start):]
}

// Shuffled "abcdefghijklmnopqrstuvwxyz0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"
const base62Alphabet = "krKL5Z0Uz9tiXh3lNsq1MFVmPcdIeoyB28vWGupQS7H6wOYDnJbfEgTxRAa4Cj"

// base62Encode encodes a number to a base62 string representation.
func base62Encode(num uint64) string {
	if num == 0 {
		return "0"
	}

	arr := []uint8{}
	base := uint64(len(base62Alphabet))

	for num > 0 {
		rem := num % base
		num = num / base
		arr = append(arr, base62Alphabet[rem])
	}

	for i, j := 0, len(arr)-1; i < j; i, j = i+1, j-1 {
		arr[i], arr[j] = arr[j], arr[i]
	}

	return string(arr)
}

type FileId uint64

func newId() FileId {
	count_path := path.Join(store_folder, "count")
	f, err := os.OpenFile(count_path, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		f, err = os.Create(count_path)
	}
	defer f.Close()

	// Lock the file for exclusive acccess:
	syscall.Flock(int(f.Fd()), syscall.LOCK_EX)

	scanner := bufio.NewScanner(f)
	scanner.Scan()
	line := scanner.Text()

	id64, err := strconv.ParseInt(line, 10, 0)
	if err != nil {
		id64 = 10000
	}
	id64++

	f.Truncate(0)
	f.Seek(0, 0)
	f.WriteString(strconv.FormatInt(id64, 10))

	return FileId(id64)
}

func createLink(local_path string, id FileId) (img_name string) {
	// Create the symlink:
	img_name = base62Encode(uint64(id))
	symlink_name := img_name + ".gif"
	symlink_path := path.Join(links_folder, symlink_name)
	log.Printf("symlink %s", symlink_path)
	os.Symlink(local_path, symlink_path)
	return
}

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		log.Fatal("Requires two arguments: local_path, id")
		return
	}

	var local_path string
	var id FileId
	if len(args) == 1 {
		local_path = args[0]
		id = newId()
	} else if len(args) == 2 {
		local_path = args[0]
		idInt, err := strconv.ParseInt(args[1], 10, 0)
		if err != nil {
			log.Fatal("Could not parse id")
			return
		}
		id = FileId(idInt)
	}

	img_name := createLink(local_path, id)
	log.Printf("%s", img_name)
}
