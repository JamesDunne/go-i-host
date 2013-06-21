package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path"
	"strconv"
	"syscall"
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

const base62Alphabet = "abcdefghijklmnopqrstuvwxyz0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"

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
	f, err := os.OpenFile("/srv/bittwiddlers.org/i-host/count", os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		f, err = os.Create("/srv/bittwiddlers.org/i-host/count")
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

var webRoot string

func postImage(rsp http.ResponseWriter, req *http.Request) {
	imgurl_s := req.FormValue("url")
	if imgurl_s != "" {
		log.Printf("%s", imgurl_s)

		// Parse the URL so we get the file name:
		imgurl, err := url.Parse(imgurl_s)
		if err != nil {
			rsp.WriteHeader(500)
			return
		}

		// Split the absolute path by dir and filename:
		_, filename := path.Split(imgurl.Path)
		log.Printf("Downloading %s", filename)

		// GET the url:
		img_rsp, err := http.Get(imgurl_s)
		if err != nil {
			rsp.WriteHeader(500)
			return
		}
		defer img_rsp.Body.Close()

		// Create a local file:
		local_path := path.Join("/srv/bittwiddlers.org/i-host", filename)
		log.Printf("to %s", local_path)

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

		// Acquire the next sequential FileId:
		id := newId()
		log.Printf("%d", int(id))

		// Create the symlink:
		symlink_name := base62Encode(uint64(id)) + ".gif"
		symlink_path := path.Join("/srv/bittwiddlers.org/i", symlink_name)
		log.Printf("symlink %s", symlink_path)
		os.Symlink(local_path, symlink_path)

		img_url := path.Join("/", symlink_name)
		log.Printf("%s", img_url)

		rsp.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(rsp, `
<!DOCTYPE html>

<html>
<head>
	<title>GIF Posted!</title>
</head>
<body style="background: black; color: silver; text-align: center; vertical-align: middle">
	<div style="height: 100%%">
		<img src="%s" alt="GIF" /><br />
		<a href="%s">link</a>
	</div>
</body>
</html>`, img_url, img_url)
		return
	}
}

func getForm(rsp http.ResponseWriter, req *http.Request) {
	rsp.Header().Set("Content-Type", "text/html; charset=utf-8")

	fmt.Fprintf(rsp, `
<!DOCTYPE html>

<html>
<head>
	<title>POST a GIF</title>
</head>
<body style="background: black; color: silver; text-align: center; vertical-align: middle">
	<form action="" method="POST">
		<label for="url">URL: <input id="url" name="url" size="128" /></label>
		<input type="submit" value="Submit" />
	</form>
</body>
</html>`)
}

// handles requests to upload images and rehost with shortened URLs
func postHandler(rsp http.ResponseWriter, req *http.Request) {
	log.Printf("Method: %s", req.Method)
	if req.Method == "POST" {
		postImage(rsp, req)
	} else {
		getForm(rsp, req)
	}
}

func main() {
	// Expect commandline arguments to specify:
	//   <listen socket type> : "unix" or "tcp" type of socket to listen on
	//   <listen address>     : network address to listen on if "tcp" or path to socket if "unix"
	//   <web root>           : absolute path prefix on URLs
	args := os.Args[1:]
	if len(args) != 3 {
		log.Fatal("Required <listen socket type> <listen address> <web root> arguments")
		return
	}

	// TODO(jsd): Make this pair of arguments a little more elegant, like "unix:/path/to/socket" or "tcp://:8080"
	socketType, socketAddr := args[0], args[1]
	webRoot = args[2]

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
