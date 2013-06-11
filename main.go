package main

import (
	//"github.com/speps/go-hashids"
	//"fmt"
	//"html"
	"crypto/sha256"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path"
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

func postImage(rsp http.ResponseWriter, req *http.Request) {
	log.Printf("POST")
	imgurl_s := req.FormValue("url")
	log.Printf("url = %s", imgurl_s)
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
		log.Printf("%s", filename)

		// GET the url:
		img_rsp, err := http.Get(imgurl_s)
		if err != nil {
			rsp.WriteHeader(500)
			return
		}
		defer img_rsp.Body.Close()

		// Create a local file:
		local_path := path.Join("/srv/bittwiddlers.org/i-host", filename)
		local_file, err := os.Create(local_path)
		if err != nil {
			rsp.WriteHeader(500)
			return
		}
		defer local_file.Close()

		// Tee the downloaded file into a hasher and copy it to a local file:
		h := sha256.New()

		mw := io.MultiWriter(local_file, h)

		// Copy the download response to the local file:
		_, err = io.Copy(mw, img_rsp.Body)
		if err != nil {
			rsp.WriteHeader(500)
			return
		}

		// Report the hash:
		hv := h.Sum(nil)
		log.Printf("%x", hv)
	}
}

func getForm(rsp http.ResponseWriter, req *http.Request) {
	rsp.Header().Add("Content-Type", "text/html; charset=utf-8")
	log.Printf("GET")
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
	//proxyRoot = args[2], args[3], args[4]

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
