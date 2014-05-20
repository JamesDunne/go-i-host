package main

import (
	"bufio"
	"fmt"
	"io"
	"path/filepath"
	//	"ioutil"
	"log"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
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

func getForm(rsp http.ResponseWriter, req *http.Request) {
	rsp.Header().Set("Content-Type", "text/html; charset=utf-8")

	fmt.Fprintf(rsp, `
<!DOCTYPE html>

<html>
<head>
	<title>POST a GIF</title>
    <meta name="viewport" content="width=device-width, initial-scale=1"/>
</head>
<body style="background: black; color: silver; text-align: center; vertical-align: middle">
	<div>
		<h2>Submit an image URL</h2>
		<form action="/" method="POST">
			<label for="url">URL: <input type="url" id="url" name="url" size="128" autofocus="autofocus" placeholder="URL" /></label><br />
			<input type="submit" value="Submit" />
		</form>
	</div>
    <div>
		<h2>Or upload an image</h2>
		<form action="/" method="POST" enctype="multipart/form-data">
			<label for="file"><input type="file" id="file" name="file" /></label><br />
			<input type="submit" value="Upload" />
		</form>
	</div>
</body>
</html>`)
}

func createLink(local_path string, id FileId) (img_name string) {
	// Create the symlink:
	img_name = base62Encode(uint64(id))
	symlink_name := img_name + ".gif"
	symlink_path := path.Join(links_folder, symlink_name)
	//log.Printf("symlink %s", symlink_path)
	os.Symlink(local_path, symlink_path)
	return
}

func isMultipart(r *http.Request) bool {
	v := r.Header.Get("Content-Type")
	if v == "" {
		return false
	}
	d, _, err := mime.ParseMediaType(v)
	if err != nil || d != "multipart/form-data" {
		return false
	}
	return true
}

func postImage(rsp http.ResponseWriter, req *http.Request) {
	// Accept file upload from client or download from URL supplied.
	var local_path string

	if isMultipart(req) {
		// Accept file upload:
		reader, err := req.MultipartReader()
		if err != nil {
			//panic(NewHttpError(http.StatusBadRequest, "Error parsing multipart form data", err))
			rsp.WriteHeader(500)
			return
		}

		// Keep reading the multipart form data and handle file uploads:
		part, err := reader.NextPart()
		if err == io.EOF {
			rsp.WriteHeader(400)
			return
		}
		if part.FileName() == "" {
			rsp.WriteHeader(400)
			return
		}

		// Copy upload data to a local file:
		local_path = path.Join(store_folder, part.FileName())
		//log.Printf("Accepting upload: '%s'\n", local_path)

		f, err := os.Create(local_path)
		if err != nil {
			//panic(NewHttpError(http.StatusInternalServerError, "Could not accept upload", fmt.Errorf("Could not create local file '%s'; %s", local_path, err.Error())))
			rsp.WriteHeader(500)
			return
		}
		defer f.Close()

		if _, err := io.Copy(f, part); err != nil {
			//panic(NewHttpError(http.StatusInternalServerError, "Could not write upload data to local file", fmt.Errorf("Could not write to local file '%s'; %s", local_path, err)))
			rsp.WriteHeader(500)
			return
		}
	} else if imgurl_s := req.FormValue("url"); imgurl_s != "" {
		// Handle download from URL:
		//log.Printf("%s", imgurl_s)

		// Parse the URL so we get the file name:
		imgurl, err := url.Parse(imgurl_s)
		if err != nil {
			rsp.WriteHeader(500)
			return
		}

		// Split the absolute path by dir and filename:
		_, filename := path.Split(imgurl.Path)
		//log.Printf("Downloading %s", filename)

		// GET the url:
		img_rsp, err := http.Get(imgurl_s)
		if err != nil {
			rsp.WriteHeader(500)
			return
		}
		defer img_rsp.Body.Close()

		// Create a local file:
		local_path = path.Join(store_folder, filename)
		//log.Printf("to %s", local_path)

		local_file, err := os.Create(local_path)
		if err != nil {
			rsp.WriteHeader(500)
			return
		}
		defer local_file.Close()

		// Download file:
		_, err = io.Copy(local_file, img_rsp.Body)
		if err != nil {
			rsp.WriteHeader(500)
			return
		}
	}

	// Acquire the next sequential FileId:
	id := newId()
	//log.Printf("%d", int(id))

	// Create the symlink:
	img_name := createLink(local_path, id)

	// Redirect to the black-background viewer:
	img_url := path.Join("/b/", img_name)
	//log.Printf("%s", img_url)

	http.Redirect(rsp, req, img_url, 302)
}

// Renders a viewing HTML page for an extensionless image request
func renderViewer(rsp http.ResponseWriter, req *http.Request) {
	imgrelpath := req.URL.Path[1:]

	bgcolor := "black"
	if startsWith(imgrelpath, "b/") {
		bgcolor = "black"
		imgrelpath = req.URL.Path[3:]
	} else if startsWith(imgrelpath, "w/") {
		bgcolor = "white"
		imgrelpath = req.URL.Path[3:]
	}

	// Find the image symlink to get its extension:
	// TODO(jsd): Sanitize file path
	matches, err := filepath.Glob(path.Join(links_folder, imgrelpath+".*"))
	if err != nil || len(matches) == 0 {
		// 404 for non-existent local file:
		//log.Printf("View: 404 %s", req.URL.Path)
		rsp.WriteHeader(404)
		return
	}

	// Get the extension of the found file and use that as the img src URL:
	img_url := "/" + imgrelpath + path.Ext(matches[0])
	//log.Printf("View: 200 %s", img_url)

	rsp.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(rsp, `
<!DOCTYPE html>

<html>
<head>
    <meta name="viewport" content="width=device-width, initial-scale=1"/>
    <style type="text/css">
html, body {
  width: 100%%;
  height: 100%%;
}
html {
  display: table;
}
body {
  display: table-cell;
  vertical-align: middle;
  text-align: center;
  background-color: %s;
  color: silver;
}
    </style>
</head>
<body>
	<div>
		<img src="%s" alt="GIF" />
	</div>
</body>
</html>`, bgcolor, img_url)
	return
}

// handles requests to upload images and rehost with shortened URLs
func postHandler(rsp http.ResponseWriter, req *http.Request) {
	//log.Printf("HTTP: %s %s", req.Method, req.URL.Path)
	if req.Method == "POST" && req.URL.Path == "/" {
		// POST a new image:
		postImage(rsp, req)
		return
	} else {
		// GET:

		if req.URL.Path == "/" {
			// GET the / form to add a new image:
			getForm(rsp, req)
			return
		}

		// Serve a GET request:
		ext := path.Ext(req.URL.Path)
		// 'ext' includes leading '.'
		if ext == "" {
			// No extension means to serve an image viewer for the image in question:
			renderViewer(rsp, req)
			return
		} else {
			// Pass request to nginx to serve static content file:
			redirPath := "/g" + req.URL.Path
			//log.Printf("X-Accel-Redirect: %s", redirPath)
			rsp.Header().Set("X-Accel-Redirect", redirPath)
			rsp.Header().Set("Content-Type", mime.TypeByExtension(ext))
			rsp.WriteHeader(200)
			return
		}
	}
}

func main() {
	// Expect commandline arguments to specify:
	//   <listen socket type> : "unix" or "tcp" type of socket to listen on
	//   <listen address>     : network address to listen on if "tcp" or path to socket if "unix"
	args := os.Args[1:]
	if len(args) != 2 {
		log.Fatal("Required <listen socket type> <listen address> arguments")
		return
	}

	// TODO(jsd): Make this pair of arguments a little more elegant, like "unix:/path/to/socket" or "tcp://:8080"
	socketType, socketAddr := args[0], args[1]

	// Create the socket to listen on:
	l, err := net.Listen(socketType, socketAddr)
	if err != nil {
		log.Fatal(err)
		return
	}

	// NOTE(jsd): Unix sockets must be unlink()ed before being reused again.

	// Handle common process-killing signals so we can gracefully shut down:
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, os.Interrupt, os.Kill, syscall.SIGTERM)
	go func(c chan os.Signal) {
		// Wait for a signal:
		sig := <-c
		log.Printf("Caught signal '%s': shutting down.", sig)
		// Stop listening:
		l.Close()
		// Delete the unix socket, if applicable:
		if socketType == "unix" {
			os.Remove(socketAddr)
		}
		// And we're done:
		os.Exit(0)
	}(sigc)

	// Start the HTTP server:
	log.Fatal(http.Serve(l, http.HandlerFunc(postHandler)))
}
