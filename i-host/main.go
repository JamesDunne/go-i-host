package main

import (
	"fmt"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path"
	"strings"
	"syscall"
)

import (
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
)

// FIXME(jsd): Hard-coded system paths here!
const (
	base_folder  = "/srv/bittwiddlers.org/i"
	db_path      = base_folder + "/sqlite.db"
	store_folder = base_folder + "/store"
	thumb_folder = base_folder + "/thumb"
)

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

	// Create an Image record:
	api, err := NewAPI()
	if err != nil {
		rsp.WriteHeader(500)
		return
	}
	defer api.Close()

	// Attempt to parse the image:
	var firstImage image.Image
	var imageKind string

	{
		imf, err := os.Open(local_path)
		if err != nil {
			rsp.WriteHeader(500)
			return
		}
		defer imf.Close()

		firstImage, imageKind, err = image.Decode(imf)
		if err != nil {
			// Bad image format or unsupported type:
			rsp.WriteHeader(400)
			rsp.Write([]byte(err.Error()))
			return
		}
	}

	var mimeType, ext string
	var encoder func(w io.Writer, m image.Image) error
	switch imageKind {
	case "jpeg":
		mimeType, ext, encoder = "image/jpeg", ".jpg", func(w io.Writer, img image.Image) error { return jpeg.Encode(w, img, &jpeg.Options{}) }
	case "png":
		mimeType, ext, encoder = "image/png", ".png", png.Encode
	case "gif":
		// TODO(jsd): Might want to rethink making GIF thumbnails.
		mimeType, ext, encoder = "image/gif", ".gif", func(w io.Writer, img image.Image) error { return gif.Encode(w, img, &gif.Options{NumColors: 256}) }
	}

	// Create the DB record:
	id, err := api.NewImage(Image{MimeType: mimeType, Title: "TODO"})
	if err != nil {
		rsp.WriteHeader(500)
		return
	}

	// Rename the file:
	img_name := base62Encode(id) + ext
	os.Rename(local_path, path.Join(path.Dir(local_path), img_name))

	// Generate the thumbnail:
	{
		thumbImg := makeThumbnail(firstImage, 200)
		tf, err := os.Create(path.Join(thumb_folder, img_name))
		if err != nil {
			rsp.WriteHeader(500)
			return
		}
		defer tf.Close()

		// Write the thumbnail to the file:
		err = encoder(tf, thumbImg)
		if err != nil {
			rsp.WriteHeader(500)
			return
		}
	}

	// Redirect to the black-background viewer:
	redir_url := path.Join("/b/", img_name)
	http.Redirect(rsp, req, redir_url, 302)
}

// Renders a viewing HTML page for an extensionless image request
func renderViewer(rsp http.ResponseWriter, req *http.Request) {
	imgrelpath := req.URL.Path[1:]

	bgcolor := "black"
	if strings.HasPrefix(imgrelpath, "b/") {
		bgcolor = "black"
		imgrelpath = req.URL.Path[3:]
	} else if strings.HasPrefix(imgrelpath, "w/") {
		bgcolor = "white"
		imgrelpath = req.URL.Path[3:]
	}

	id := base62Decode(imgrelpath)
	api, err := NewAPI()
	if err != nil {
		rsp.WriteHeader(500)
		return
	}
	defer api.Close()

	_, err = api.GetImage(id)
	if err != nil {
		rsp.WriteHeader(404)
		return
	}

	//img.ImagePath

	// Get the extension of the found file and use that as the img src URL:
	img_url := "/" + imgrelpath + ".gif"

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
