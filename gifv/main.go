package main

import (
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

var (
	base_folder = "."
)

const thumbnail_dimensions = 200

func store_folder() string { return base_folder + "/store" }
func thumb_folder() string { return base_folder + "/thumb" }
func tmp_folder() string   { return base_folder + "/tmp" }

// Random number state.
// We generate random temporary file names so that there's a good
// chance the file doesn't exist yet - keeps the number of tries in
// TempFile to a minimum.
var rand uint32
var randmu sync.Mutex

func reseed() uint32 {
	return uint32(time.Now().UnixNano() + int64(os.Getpid()))
}

func nextSuffix() string {
	randmu.Lock()
	r := rand
	if r == 0 {
		r = reseed()
	}
	r = r*1664525 + 1013904223 // constants from Numerical Recipes
	rand = r
	randmu.Unlock()
	return strconv.Itoa(int(1e9 + r%1e9))[1:]
}

// TempFile creates a new temporary file in the directory dir
// with a name beginning with prefix, opens the file for reading
// and writing, and returns the resulting *os.File.
// If dir is the empty string, TempFile uses the default directory
// for temporary files (see os.TempDir).
// Multiple programs calling TempFile simultaneously
// will not choose the same file.  The caller can use f.Name()
// to find the pathname of the file.  It is the caller's responsibility
// to remove the file when no longer needed.
func TempFile(dir, prefix, ext string) (f *os.File, err error) {
	if dir == "" {
		dir = os.TempDir()
	}

	nconflict := 0
	for i := 0; i < 10000; i++ {
		name := filepath.Join(dir, prefix+nextSuffix()+ext)
		f, err = os.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
		if os.IsExist(err) {
			if nconflict++; nconflict > 10 {
				rand = reseed()
			}
			continue
		}
		break
	}
	return
}

func downloadFile(url string) (string, error) {
	// Do a HTTP GET to fetch the image:
	img_rsp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer img_rsp.Body.Close()

	// Create a local temporary file to download to:
	os.MkdirAll(tmp_folder(), 0755)
	local_file, err := TempFile(tmp_folder(), "dl-", "")
	if err != nil {
		return "", err
	}
	defer local_file.Close()

	// Download file:
	_, err = io.Copy(local_file, img_rsp.Body)
	if err != nil {
		return "", err
	}

	return local_file.Name(), nil
}

func moveToStoreFolder(local_path string, id int64, ext string) (err error) {
	// Move and rename the file:
	img_name := strconv.FormatInt(id, 10)
	os.MkdirAll(store_folder(), 0755)
	store_path := path.Join(store_folder(), img_name+ext)
	if err = os.Rename(local_path, store_path); err != nil {
		return
	}
	return nil
}

func main() {
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
		wg.Add(3)
		fetch_func := func(ext string, path *string) {
			defer wg.Done()

			// Fetch the GIF version:
			var err error
			*path, err = downloadFile("http://i.imgur.com/" + imgur_id + ext)
			if err != nil {
				fmt.Println(err)
				return
			}
		}

		var (
			gif_file  string
			webm_file string
			mp4_file  string
		)

		go fetch_func(".gif", &gif_file)
		go fetch_func(".webm", &webm_file)
		go fetch_func(".mp4", &mp4_file)

		// Function to run after DB record creation:
		// Wait for all files to download:
		wg.Wait()

		// Move temp files to final storage:
		err = moveToStoreFolder(gif_file, id, ".gif")
		if err != nil {
			fmt.Println(err)
			return
		}
		err = moveToStoreFolder(webm_file, id, ".webm")
		if err != nil {
			fmt.Println(err)
			return
		}
		err = moveToStoreFolder(mp4_file, id, ".mp4")
		if err != nil {
			fmt.Println(err)
			return
		}
	}
}
