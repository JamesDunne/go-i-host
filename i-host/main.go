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
	//base_folder  = "/srv/bittwiddlers.org/i"
	base_folder  = "."
	db_path      = base_folder + "/sqlite.db"
	store_folder = base_folder + "/store"
	thumb_folder = base_folder + "/thumb"
	tmp_folder   = base_folder + "/tmp"
)

const nginxAccelRedirect = false

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

func imageKindTo(imageKind string) (ext, mimeType string) {
	switch imageKind {
	case "jpeg":
		return "image/jpeg", ".jpg"
	case "png":
		return "image/png", ".png"
	case "gif":
		return "image/gif", ".gif"
	}
	return "", ""
}

func webErrorIf(rsp http.ResponseWriter, err error, statusCode int) bool {
	if err == nil {
		return false
	}

	rsp.WriteHeader(statusCode)
	rsp.Write([]byte(err.Error()))
	return true
}

func postImage(rsp http.ResponseWriter, req *http.Request) {
	// Accept file upload from client or download from URL supplied.
	var local_path string

	if isMultipart(req) {
		// Accept file upload:
		reader, err := req.MultipartReader()
		if webErrorIf(rsp, err, 500) {
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
		local_path = path.Join(tmp_folder, part.FileName())
		//log.Printf("Accepting upload: '%s'\n", local_path)

		if err, statusCode := func() (error, int) {
			f, err := os.Create(local_path)
			if err != nil {
				return err, 500
			}
			defer f.Close()

			if _, err := io.Copy(f, part); err != nil {
				return err, 500
			}
			return nil, 200
		}(); webErrorIf(rsp, err, statusCode) {
			return
		}
	} else if imgurl_s := req.FormValue("url"); imgurl_s != "" {
		// Handle download from URL:
		//log.Printf("%s", imgurl_s)

		// Parse the URL so we get the file name:
		imgurl, err := url.Parse(imgurl_s)
		if webErrorIf(rsp, err, 500) {
			return
		}

		// Split the absolute path by dir and filename:
		_, filename := path.Split(imgurl.Path)
		//log.Printf("Downloading %s", filename)

		// GET the url:
		if err, statusCode := func() (error, int) {
			img_rsp, err := http.Get(imgurl_s)
			if err != nil {
				return err, 500
			}
			defer img_rsp.Body.Close()

			// Create a local file:
			local_path = path.Join(tmp_folder, filename)
			//log.Printf("to %s", local_path)

			local_file, err := os.Create(local_path)
			if err != nil {
				return err, 500
			}
			defer local_file.Close()

			// Download file:
			_, err = io.Copy(local_file, img_rsp.Body)
			if err != nil {
				return err, 500
			}

			return nil, 200
		}(); webErrorIf(rsp, err, statusCode) {
			return
		}
	}

	// Open the database:
	api, err := NewAPI()
	if webErrorIf(rsp, err, 500) {
		return
	}
	defer api.Close()

	// Attempt to parse the image:
	var firstImage image.Image
	var imageKind string

	if err, statusCode := func() (error, int) {
		imf, err := os.Open(local_path)
		if err != nil {
			return err, 500
		}
		defer imf.Close()

		firstImage, imageKind, err = image.Decode(imf)
		if err != nil {
			return err, 400
		}

		return nil, 200
	}(); webErrorIf(rsp, err, statusCode) {
		return
	}

	var ext string
	var encoder func(w io.Writer, m image.Image) error

	_, ext = imageKindTo(imageKind)

	switch imageKind {
	case "jpeg":
		encoder = func(w io.Writer, img image.Image) error { return jpeg.Encode(w, img, &jpeg.Options{Quality: 100}) }
	case "png":
		encoder = png.Encode
	case "gif":
		// TODO(jsd): Might want to rethink making GIF thumbnails.
		encoder = func(w io.Writer, img image.Image) error { return gif.Encode(w, img, &gif.Options{NumColors: 256}) }
	}

	// Create the DB record:
	id, err := api.NewImage(Image{Kind: imageKind, Title: "TODO"})
	if webErrorIf(rsp, err, 500) {
		return
	}

	// Rename the file:
	img_name := base62Encode(10000 + id)
	if err := os.Rename(local_path, path.Join(store_folder, img_name+ext)); webErrorIf(rsp, err, 500) {
		return
	}

	// Generate the thumbnail:
	if err, statusCode := func() (error, int) {
		thumbImg := makeThumbnail(firstImage, 200)
		tf, err := os.Create(path.Join(thumb_folder, img_name+ext))
		if err != nil {
			return err, 500
		}
		defer tf.Close()

		// Write the thumbnail to the file:
		err = encoder(tf, thumbImg)
		if err != nil {
			return err, 500
		}

		return nil, 200
	}(); webErrorIf(rsp, err, statusCode) {
		return
	}

	// Redirect to the black-background viewer:
	redir_url := path.Join("/b/", img_name)
	http.Redirect(rsp, req, redir_url, 302)
}

func getForm(rsp http.ResponseWriter, req *http.Request) {
	rsp.Header().Set("Content-Type", "text/html; charset=utf-8")

	fmt.Fprintf(rsp, `
<!DOCTYPE html>

<html>
<head>
	<title>POST an Image</title>
    <meta name="viewport" content="width=device-width, initial-scale=1"/>
</head>
<body style="background: black; color: silver; text-align: center; vertical-align: middle">
	<div>
		<h2>Submit an image URL</h2>
		<form action="/new" method="POST">
			<label for="url">URL: <input type="url" id="url" name="url" size="128" autofocus="autofocus" placeholder="URL" /></label><br />
			<input type="submit" value="Submit" />
		</form>
	</div>
    <div>
		<h2>Or upload an image</h2>
		<form action="/new" method="POST" enctype="multipart/form-data">
			<label for="file"><input type="file" id="file" name="file" /></label><br />
			<input type="submit" value="Upload" />
		</form>
	</div>
</body>
</html>`)
}

// handles requests to upload images and rehost with shortened URLs
func requestHandler(rsp http.ResponseWriter, req *http.Request) {
	//log.Printf("HTTP: %s %s", req.Method, req.URL.Path)
	if req.Method == "POST" {
		// POST:

		if req.URL.Path == "/new" {
			// POST a new image:
			postImage(rsp, req)
			return
		}

		rsp.WriteHeader(http.StatusBadRequest)
		return
	}

	// GET:
	if req.URL.Path == "/favicon.ico" {
		rsp.WriteHeader(http.StatusNoContent)
		return
	} else if req.URL.Path == "/" {
		// Render a list page:
		api, err := NewAPI()
		if webErrorIf(rsp, err, 500) {
			return
		}
		defer api.Close()

		list, err := api.GetList()
		if webErrorIf(rsp, err, 500) {
			return
		}

		rsp.Header().Set("Content-Type", "text/html; charset=utf-8")
		rsp.WriteHeader(200)
		rsp.Write([]byte(`
<!DOCTYPE html>

<html>
<head>
    <meta name="viewport" content="width=device-width, initial-scale=1"/>
    <title>Listing</title>

<style type="text/css">
html, body {
  width: 100%;
  height: 100%;
}
html {
  display: table;
}
body {
  display: table-cell;
  vertical-align: middle;
  text-align: left;
  background-color: black;
  color: silver;
}
li {
  list-style-type: none;
}
</style>
</head>
<body>
    <div>
        <ul>`))
		for _, img := range list {
			_, ext := imageKindTo(img.Kind)
			// TODO(jsd): HTML encoding!
			rsp.Write([]byte(fmt.Sprintf(`<li><a href="/b/%[1]s"><img src="/t/%[1]s%[2]s" alt="%[3]s" title="%[3]s" /></a></li>`+"\n", base62Encode(img.ID+10000), ext, img.Title)))
		}
		rsp.Write([]byte(`
        </ul>
    </div>
</body>
</html>`))
		return
	} else if req.URL.Path == "/new" {
		// GET the / form to add a new image:
		getForm(rsp, req)
		return
	}

	dir := path.Dir(req.URL.Path)

	// Look up the image's record by base62 encoded ID:
	filename := path.Base(req.URL.Path)
	filename = filename[0 : len(filename)-len(path.Ext(req.URL.Path))]

	id := base62Decode(filename) - 10000

	api, err := NewAPI()
	if webErrorIf(rsp, err, 500) {
		return
	}
	defer api.Close()
	img, err := api.GetImage(id)
	if webErrorIf(rsp, err, 500) {
		return
	}
	if img == nil {
		rsp.WriteHeader(http.StatusNotFound)
		return
	}

	// Determine mime-type and file extension:
	mime, ext := imageKindTo(img.Kind)

	// Find the image file:
	img_name := base62Encode(id+10000) + ext

	if dir == "/b" || dir == "/w" {
		// Render a black or white BG centered image viewer:
		var bgcolor string
		switch dir {
		case "/b":
			bgcolor = "black"
		case "/w":
			bgcolor = "white"
		}

		rsp.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(rsp, `
<!DOCTYPE html>

<html>
<head>
    <meta name="viewport" content="width=device-width, initial-scale=1"/>
    <meta property="og:site_name" content="i.bittwiddlers.org"/>
    <meta property="og:title" content="%[3]s"/>
    <meta property="og:description" content=""/>
    <meta property="og:image" content="/t/%[2]s">
    <meta property="og:url" content="http://i.bittwiddlers.org/%[2]s">
    <meta property="og:type" content="video.other"/>

    <title>%[3]s</title>

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
  background-color: %[1]s;
  color: silver;
}
</style>
</head>
<body>
	<div><img src="/%[2]s" alt="%[3]s" title="%[3]s" /></div>
</body>
</html>`, bgcolor, img_name, img.Title)
		// TODO(jsd): HTML encoding!
		return
	} else if dir == "/t" {
		thumb_path := path.Join(thumb_folder, img_name)

		rsp.Header().Set("Content-Type", mime)
		http.ServeFile(rsp, req, thumb_path)
		return
	}

	// Serve actual image contents:
	if nginxAccelRedirect {
		// Pass request to nginx to serve static content file:
		redirPath := "/g/" + img_name

		//log.Printf("X-Accel-Redirect: %s", redirPath)
		rsp.Header().Set("X-Accel-Redirect", redirPath)
		rsp.Header().Set("Content-Type", mime)
		rsp.WriteHeader(200)
		return
	} else {
		// Serve content directly with the proper mime-type:
		local_path := path.Join(store_folder, img_name)

		rsp.Header().Set("Content-Type", mime)
		http.ServeFile(rsp, req, local_path)
		return
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
	log.Fatal(http.Serve(l, http.HandlerFunc(requestHandler)))
}
