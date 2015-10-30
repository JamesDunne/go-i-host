package main

import (
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"strconv"
	"sync"
)

var (
	base_folder = "."
)

const thumbnail_dimensions = 200

func store_folder() string { return base_folder + "/store" }
func thumb_folder() string { return base_folder + "/thumb" }

func downloadFile(url string, id int64, ext string) (string, error) {
	// Create a local file to download to:
	img_name := strconv.FormatInt(id, 10)
	store_path := path.Join(store_folder(), img_name+ext)

	fmt.Printf("Downloading %s to %s\n", url, store_path)

	local_file, err := os.OpenFile(store_path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)

	// File already exists:
	if os.IsExist(err) {
		fmt.Printf("File exists...\n")
		var fi os.FileInfo
		var head_rsp *http.Response

		// Check file size:
		fi, err = os.Stat(store_path)
		file_size := fi.Size()

		fmt.Printf("File size = %d\n", file_size)

		// HEAD to get download file size:
		head_rsp, err = http.Head(url)
		if err == nil {
			fmt.Printf("HEAD reports size = %d\n", head_rsp.ContentLength)

			if head_rsp.ContentLength > 0 {
				if file_size == head_rsp.ContentLength {
					fmt.Println("Same file size; skip")
					// File is same size; do nothing:
					return store_path, nil
				}
			}

			// File is not same size; redownload:
			fmt.Println("Redownload")
			local_file, err = os.OpenFile(store_path, os.O_RDWR|os.O_TRUNC|os.O_EXCL, 0600)
		}
	}
	if err != nil {
		return "", err
	}
	defer local_file.Close()

	// Do a HTTP GET to fetch the image:
	img_rsp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer img_rsp.Body.Close()

	// Download file:
	_, err = io.Copy(local_file, img_rsp.Body)
	if err != nil {
		return "", err
	}

	return local_file.Name(), nil
}

func main() {
	os.MkdirAll(store_folder(), 0755)

	file, err := os.Open("images.csv")
	if err != nil {
		fmt.Println(err)
		return
	}
	defer file.Close()

	csvr := csv.NewReader(file)
	imgs, err := csvr.ReadAll()
	if err != nil {
		fmt.Println(err)
		return
	}

	for _, img := range imgs {
		id, err := strconv.ParseInt(img[0], 10, 64)
		if err != nil {
			fmt.Println(err)
			return
		}

		imgur_id := img[1]
		fmt.Printf("%d: %s\n", id, imgur_id)

		// Background-fetch the GIF, WEBM, and MP4 files:
		wg := &sync.WaitGroup{}
		fetch_func := func(ext string) {
			defer wg.Done()

			// Fetch the file:
			var err error
			path, err := downloadFile("http://i.imgur.com/"+imgur_id+ext, id, ext)
			if err != nil {
				fmt.Println(err)
				return
			}
			fmt.Println(path)
		}

		wg.Add(2)
		go fetch_func(".webm")
		go fetch_func(".mp4")

		// Function to run after DB record creation:
		// Wait for all files to download:
		wg.Wait()
	}
}
