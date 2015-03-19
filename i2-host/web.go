package main

import (
	"crypto/sha256"
	"encoding/gob"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

import "github.com/JamesDunne/go-util/web"

func imageKindTo(imageKind string) (mimeType, ext, thumbExt string) {
	switch imageKind {
	case "jpeg":
		return "image/jpeg", ".jpg", ".jpg"
	case "png":
		return "image/png", ".png", ".png"
	case "gif":
		return "image/gif", ".gif", ".png"
	}
	return "", "", ""
}

func filename(path string) string {
	return path[:len(path)-len(filepath.Ext(path))]
}

type ImageViewModel struct {
	ID             int64   `json:"id"`
	Base62ID       string  `json:"base62id"`
	Title          string  `json:"title"`
	Kind           string  `json:"kind"`
	ImageURL       string  `json:"imageURL"`
	ThumbURL       string  `json:"thumbURL"`
	Submitter      string  `json:"submitter,omitempty"`
	CollectionName string  `json:"collectionName,omitempty"`
	SourceURL      *string `json:"sourceURL,omitempty"`
	RedirectToID   *int64  `json:"redirectToID,omitempty"`
	IsClean        bool    `json:"isClean"`
	Keywords       string  `json:"keywords,omitempty"`
}

func xlatImageViewModel(i *Image, o *ImageViewModel) *ImageViewModel {
	if o == nil {
		o = new(ImageViewModel)
	}

	o.ID = i.ID
	o.Base62ID = b62.Encode(i.ID + 10000)
	o.Title = i.Title
	o.Kind = i.Kind
	o.SourceURL = i.SourceURL
	o.RedirectToID = i.RedirectToID
	o.IsClean = i.IsClean
	o.CollectionName = i.CollectionName
	o.Submitter = i.Submitter
	o.Keywords = i.Keywords

	if o.Kind == "" {
		o.Kind = "gif"
	}
	_, ext, thumbExt := imageKindTo(o.Kind)
	switch o.Kind {
	case "youtube":
		o.ImageURL = "//www.youtube.com/embed/" + *i.SourceURL
		o.ThumbURL = "//i1.ytimg.com/vi/" + *i.SourceURL + "/hqdefault.jpg"
		break
	case "imgur-gifv":
		hash := filename(*i.SourceURL)
		o.ImageURL = "//i.imgur.com/" + hash + ".mp4"
		o.ThumbURL = "//i.imgur.com/" + hash + "b.jpg"
		break
	default:
		o.ImageURL = "/" + o.Base62ID + ext
		o.ThumbURL = "/t/" + o.Base62ID + thumbExt
		break
	}

	return o
}

func projectModelList(list []Image) (modelList []ImageViewModel) {
	modelList = make([]ImageViewModel, 0, len(list))

	count := 0
	for i, _ := range list {
		img := &list[i]
		if img.IsHidden {
			continue
		}

		modelList = append(modelList, ImageViewModel{})
		xlatImageViewModel(img, &modelList[count])
		count++
	}

	return
}

func useAPI(use func(api *API) *web.Error) *web.Error {
	api, err := NewAPI()
	if err != nil {
		return web.AsError(err, http.StatusInternalServerError)
	}
	defer api.Close()

	return use(api)
}

type imageStoreRequest struct {
	Kind      string `json:"kind"`
	Title     string `json:"title"`
	SourceURL string `json:"sourceURL"`
	Submitter string `json:"submitter"`
	IsClean   bool   `json:"isClean"`
	Keywords  string `json:"keywords"`

	CollectionName string
	LocalPath      string
}

func storeImage(req *imageStoreRequest) (id int64, werr *web.Error) {
	if req.Title == "" {
		return 0, web.AsError(fmt.Errorf("Missing title!"), http.StatusBadRequest)
	}

	// Open the database:
	werr = useAPI(func(api *API) (werr *web.Error) {
		var err error

		newImage := &Image{
			Kind:           req.Kind,
			Title:          req.Title,
			SourceURL:      &req.SourceURL,
			CollectionName: req.CollectionName,
			Submitter:      req.Submitter,
			IsClean:        req.IsClean,
			Keywords:       strings.ToLower(req.Keywords),
		}

		// Generate keywords from title:
		if newImage.Keywords == "" {
			newImage.Keywords = titleToKeywords(newImage.Title)
		}

		if req.LocalPath != "" {
			// Do some local image processing first:
			var firstImage image.Image

			firstImage, newImage.Kind, err = decodeFirstImage(req.LocalPath)
			defer func() { firstImage = nil }()
			if werr = web.AsError(err, http.StatusInternalServerError); werr != nil {
				return
			}

			if newImage.Kind == "" {
				newImage.Kind = "gif"
			}
			_, ext, thumbExt := imageKindTo(newImage.Kind)

			// Create the DB record:
			id, err = api.NewImage(newImage)
			if werr = web.AsError(err, http.StatusInternalServerError); werr != nil {
				return
			}

			// Rename the file:
			img_name := strconv.FormatInt(id, 10)
			os.MkdirAll(store_folder(), 0755)
			store_path := path.Join(store_folder(), img_name+ext)
			if werr = web.AsError(os.Rename(req.LocalPath, store_path), http.StatusInternalServerError); werr != nil {
				return
			}

			// Generate a thumbnail:
			os.MkdirAll(thumb_folder(), 0755)
			thumb_path := path.Join(thumb_folder(), img_name+thumbExt)
			if werr = web.AsError(generateThumbnail(firstImage, newImage.Kind, thumb_path), http.StatusInternalServerError); werr != nil {
				return
			}
		} else {
			if newImage.Kind == "" {
				newImage.Kind = "gif"
			}

			// Create the DB record:
			id, err = api.NewImage(newImage)
			if werr = web.AsError(err, http.StatusInternalServerError); werr != nil {
				return
			}
		}

		return nil
	})
	if werr != nil {
		return 0, werr
	}

	return id, nil
}

func downloadImageFor(store *imageStoreRequest) *web.Error {
	// Validate the URL:
	imgurl, err := url.Parse(store.SourceURL)
	if err != nil {
		return web.AsError(err, http.StatusBadRequest)
	}

	// Check if it's a youtube link:
	if (imgurl.Scheme == "http" || imgurl.Scheme == "https") && (imgurl.Host == "www.youtube.com") {
		// Process youtube links specially:
		if imgurl.Path != "/watch" {
			return web.AsError(fmt.Errorf("Unrecognized YouTube URL form."), http.StatusBadRequest)
		}

		store.Kind = "youtube"
		store.LocalPath = ""
		store.SourceURL = imgurl.Query().Get("v")
		return nil
	}

	// Check for imgur's gifv format:
	if (imgurl.Scheme == "http" || imgurl.Scheme == "https") && (imgurl.Host == "i.imgur.com") && (filepath.Ext(imgurl.Path) == ".gifv") {
		store.Kind = "imgur-gifv"
		store.LocalPath = ""
		store.SourceURL = filename(imgurl.Path)
		return nil
	}

	// Do a HTTP GET to fetch the image:
	img_rsp, err := http.Get(store.SourceURL)
	if err != nil {
		return web.AsError(err, http.StatusInternalServerError)
	}
	defer img_rsp.Body.Close()

	// Create a local temporary file to download to:
	os.MkdirAll(tmp_folder(), 0755)
	local_file, err := TempFile(tmp_folder(), "dl-", "")
	if err != nil {
		return web.AsError(err, http.StatusInternalServerError)
	}
	defer local_file.Close()

	store.LocalPath = local_file.Name()

	// Download file:
	_, err = io.Copy(local_file, img_rsp.Body)
	if err != nil {
		return web.AsError(err, http.StatusInternalServerError)
	}

	return nil
}

func getForm(rsp http.ResponseWriter, req *http.Request) {
	rsp.Header().Set("Content-Type", "text/html; charset=utf-8")
}

func listCollection(rsp http.ResponseWriter, req *http.Request, keywords []string, collectionName string, list []Image, nsfw bool) {
	cached, werr := doCaching(req, rsp, struct {
		CollectionName string
		List           []Image
		ShowUnclean    bool
	}{
		CollectionName: collectionName,
		List:           list,
		ShowUnclean:    nsfw,
	})
	if werr != nil {
		return
	}
	if cached {
		return
	}

	// Project into a view model:
	model := struct {
		List        []ImageViewModel
		ShowUnclean bool
		Keywords    string
	}{
		List:        projectModelList(list),
		ShowUnclean: nsfw,
		Keywords:    strings.Join(keywords, " "),
	}

	rsp.Header().Set("Content-Type", "text/html; charset=utf-8")
	rsp.WriteHeader(200)
	if web.AsError(uiTmpl.ExecuteTemplate(rsp, "list", model), http.StatusInternalServerError).AsHTML().Respond(rsp) {
		return
	}
	return
}

func getImage(id int64) (img *Image, werr *web.Error) {
	werr = useAPI(func(api *API) *web.Error {
		var err error

		img, err = api.GetImage(id)

		return web.AsError(err, http.StatusInternalServerError)
	})

	return
}

func getList(collectionName string, includeBase bool, orderBy ImagesOrderBy) (list []Image, werr *web.Error) {
	werr = useAPI(func(api *API) *web.Error {
		var err error

		list, err = api.GetList(collectionName, includeBase, orderBy)

		return web.AsError(err, http.StatusInternalServerError)
	})

	return
}

func apiSearch(keywords []string, collectionName string, includeBase bool, orderBy ImagesOrderBy) (list []Image, werr *web.Error) {
	werr = useAPI(func(api *API) *web.Error {
		var err error

		list, err = api.Search(keywords, collectionName, includeBase, orderBy)

		return web.AsError(err, http.StatusInternalServerError)
	})

	return
}

func doCaching(req *http.Request, rsp http.ResponseWriter, data interface{}) (bool, *web.Error) {
	// Calculate ETag of data as hex(SHA256(gob(data))):
	sha := sha256.New()
	err := gob.NewEncoder(sha).Encode(data)
	if err != nil {
		return false, web.AsError(err, http.StatusInternalServerError)
	}

	etag := "\"" + hex.EncodeToString(sha.Sum(nil)) + "\""

	if check_etag := req.Header.Get("If-None-Match"); check_etag == etag {
		// 304 Not Modified:
		rsp.WriteHeader(http.StatusNotModified)
		return true, nil
	}

	rsp.Header().Set("ETag", etag)
	return false, nil
}

func apiListResult(req *http.Request, rsp http.ResponseWriter, list []Image, werr *web.Error) *web.Error {
	if werr != nil {
		return werr.AsJSON()
	}

	cached, werr := doCaching(req, rsp, list)
	if werr != nil {
		return werr.AsJSON()
	}
	if cached {
		return nil
	}

	// Project into a view model:
	model := struct {
		List []ImageViewModel `json:"list"`
	}{
		List: projectModelList(list),
	}

	web.JsonSuccess(rsp, &model)
	return nil
}

type viewTemplateModel struct {
	BGColor    string
	FillScreen bool
	Query      map[string]string
	Image      ImageViewModel
	IsAdmin    bool
}

func flattenQuery(query map[string][]string) (flat map[string]string) {
	flat = make(map[string]string)
	for k, v := range query {
		flat[k] = v[0]
	}
	return
}

// handles requests to upload images and rehost with shortened URLs
func requestHandler(rsp http.ResponseWriter, req *http.Request) *web.Error {
	// Set RemoteAddr for forwarded requests:
	{
		ip := req.Header.Get("X-Real-IP")
		if ip == "" {
			ip = req.Header.Get("X-Forwarded-For")
		}
		if ip != "" {
			req.RemoteAddr = ip
		}
	}
	//log.Printf("%s %s %s %s\nHeaders: %v\n\n", req.RemoteAddr, req.Method, req.Host, req.URL, req.Header)

	if req.Method == "POST" {
		// POST:

		if collectionName, ok := web.MatchSimpleRoute(req.URL.Path, "/col/add"); ok {
			// Add a new image via URL to download from:
			imgurl_s := req.FormValue("url")
			if imgurl_s == "" {
				return web.AsError(fmt.Errorf("Missing required 'url' form value!"), http.StatusBadRequest).AsHTML()
			}

			log.Printf("nsfw='%s'\n", req.FormValue("nsfw"))
			nsfw := req.FormValue("nsfw") == "1"

			store := &imageStoreRequest{
				CollectionName: collectionName,
				Submitter:      req.RemoteAddr,
				Title:          req.FormValue("title"),
				SourceURL:      imgurl_s,
				Keywords:       strings.ToLower(req.FormValue("keywords")),
				IsClean:        !nsfw,
			}

			// Require the 'title' form value:
			if store.Title == "" {
				return web.AsError(fmt.Errorf("Missing title!"), http.StatusBadRequest).AsHTML()
			}

			// Download the image from the URL:
			if werr := downloadImageFor(store); werr != nil {
				return werr.AsHTML()
			}

			// Store it in the database and generate thumbnail:
			id, werr := storeImage(store)
			if werr != nil {
				return werr.AsHTML()
			}

			// Redirect to a black-background view of the image:
			redir_url := path.Join("/b/", b62.Encode(id+10000))
			http.Redirect(rsp, req, redir_url, http.StatusFound)
			return nil
		} else if collectionName, ok := web.MatchSimpleRoute(req.URL.Path, "/col/upload"); ok {
			// Upload a new image:
			store := &imageStoreRequest{
				CollectionName: collectionName,
				Submitter:      req.RemoteAddr,
			}

			if !web.IsMultipart(req) {
				return web.AsError(fmt.Errorf("Upload request must be multipart form data"), http.StatusBadRequest).AsHTML()
			}

			// Accept file upload:
			reader, err := req.MultipartReader()
			if werr := web.AsError(err, http.StatusBadRequest); werr != nil {
				return werr.AsHTML()
			}

			// Keep reading the multipart form data and handle file uploads:
			for {
				part, err := reader.NextPart()
				if err == io.EOF {
					break
				}

				// Parse normal form values:
				if part.FormName() == "title" {
					// TODO: parse content-length if it exists?
					//part.Header.Get("Content-Length")

					t, err := ioutil.ReadAll(part)
					if werr := web.AsError(err, http.StatusInternalServerError); werr != nil {
						return werr.AsHTML()
					}
					store.Title = string(t)
					continue
				} else if part.FormName() == "keywords" {
					t, err := ioutil.ReadAll(part)
					if werr := web.AsError(err, http.StatusInternalServerError); werr != nil {
						return werr.AsHTML()
					}
					store.Keywords = strings.ToLower(string(t))
					continue
				} else if part.FormName() == "nsfw" {
					t, err := ioutil.ReadAll(part)
					if werr := web.AsError(err, http.StatusInternalServerError); werr != nil {
						return werr.AsHTML()
					}
					nsfw := (string(t) == "1")
					store.IsClean = !nsfw
					continue
				}

				if part.FileName() == "" {
					continue
				}

				// Copy upload data to a local file:
				store.SourceURL = "file://" + part.FileName()

				return func() *web.Error {
					os.MkdirAll(tmp_folder(), 0755)
					f, err := TempFile(tmp_folder(), "up-", path.Ext(part.FileName()))
					if err != nil {
						return web.AsError(err, http.StatusInternalServerError)
					}
					defer f.Close()

					store.LocalPath = f.Name()

					if _, err := io.Copy(f, part); err != nil {
						return web.AsError(err, http.StatusInternalServerError)
					}
					return nil
				}().AsHTML()
			}

			// Store it in the database and generate thumbnail:
			id, werr := storeImage(store)
			if werr.AsHTML().Respond(rsp) {
				return werr
			}

			// Redirect to a black-background view of the image:
			redir_url := path.Join("/b/", b62.Encode(id+10000))
			http.Redirect(rsp, req, redir_url, http.StatusFound)
			return nil
		} else if id_s, ok := web.MatchSimpleRoute(req.URL.Path, "/admin/update"); ok {
			id := b62.Decode(id_s) - 10000

			if req.FormValue("delete") != "" {
				if werr := useAPI(func(api *API) *web.Error {
					var err error
					err = api.Delete(id)
					return web.AsError(err, http.StatusInternalServerError)
				}); werr != nil {
					return werr.AsHTML()
				}

				// Redirect back to edit page:
				http.Redirect(rsp, req, "/admin/edit/"+id_s, http.StatusFound)
				return nil
			}

			var img *Image
			if werr := useAPI(func(api *API) *web.Error {
				var err error
				img, err = api.GetImage(id)
				return web.AsError(err, http.StatusInternalServerError)
			}); werr != nil {
				return werr.AsHTML()
			}
			if img == nil {
				return web.AsError(fmt.Errorf("Could not find image by ID"), http.StatusNotFound).AsHTML()
			}

			img.Title = req.FormValue("title")
			img.Keywords = strings.ToLower(req.FormValue("keywords"))
			img.CollectionName = req.FormValue("collection")
			img.Submitter = req.FormValue("submitter")
			img.IsClean = (req.FormValue("nsfw") == "")

			// Generate keywords from title:
			if img.Keywords == "" {
				img.Keywords = titleToKeywords(img.Title)
			}

			// Process the update request:
			if werr := useAPI(func(api *API) *web.Error {
				return web.AsError(api.Update(img), http.StatusInternalServerError)
			}); werr != nil {
				return werr.AsHTML()
			}

			// Redirect back to edit page:
			http.Redirect(rsp, req, "/admin/edit/"+id_s, http.StatusFound)
			return nil
		} else if collectionName, ok := web.MatchSimpleRoute(req.URL.Path, "/api/v1/add"); ok {
			// Add a new image via URL to download from via JSON API:
			store := &imageStoreRequest{
				CollectionName: collectionName,
			}

			jd := json.NewDecoder(req.Body)
			err := jd.Decode(store)
			if werr := web.AsError(err, http.StatusBadRequest); werr != nil {
				return werr.AsJSON()
			}

			// Download Image locally:
			if werr := downloadImageFor(store); werr != nil {
				return werr.AsJSON()
			}

			// Process the store request:
			id, werr := storeImage(store)
			if werr != nil {
				return werr.AsJSON()
			}

			web.JsonSuccess(rsp, &struct {
				ID       int64  `json:"id"`
				Base62ID string `json:"base62id"`
			}{
				ID:       id,
				Base62ID: b62.Encode(id + 10000),
			})
			return nil
		} else if id_s, ok := web.MatchSimpleRoute(req.URL.Path, "/api/v1/update"); ok {
			// TODO: Lock down based on basic_auth.username == collectionName.

			id := b62.Decode(id_s) - 10000

			// Get existing image for update:
			img, werr := getImage(id)
			if werr != nil {
				return werr.AsJSON()
			}

			// Decode JSON straight onto existing Image record:
			jd := json.NewDecoder(req.Body)
			err := jd.Decode(img)
			if werr := web.AsError(err, http.StatusBadRequest); werr != nil {
				return werr.AsJSON()
			}

			// Generate keywords from title:
			if img.Keywords == "" {
				img.Keywords = titleToKeywords(img.Title)
			} else {
				img.Keywords = strings.ToLower(img.Keywords)
			}

			// Process the update request:
			if werr := useAPI(func(api *API) *web.Error {
				return web.AsError(api.Update(img), http.StatusInternalServerError)
			}); werr != nil {
				return werr.AsJSON()
			}

			web.JsonSuccess(rsp, &struct {
				Success bool `json:"success"`
			}{
				Success: true,
			})
			return nil
		} else if id_s, ok := web.MatchSimpleRoute(req.URL.Path, "/api/v1/delete"); ok {
			// TODO: Lock down based on basic_auth.username == collectionName.

			id := b62.Decode(id_s) - 10000

			if werr := useAPI(func(api *API) *web.Error {
				return web.AsError(api.Delete(id), http.StatusInternalServerError)
			}); werr != nil {
				return werr.AsJSON()
			}

			web.JsonSuccess(rsp, &struct {
				Success bool `json:"success"`
			}{
				Success: true,
			})
			return nil
		} else if id_s, ok := web.MatchSimpleRoute(req.URL.Path, "/api/v1/crop"); ok {
			id := b62.Decode(id_s) - 10000

			cr := &struct {
				Left   int `json:"left"`
				Top    int `json:"top"`
				Right  int `json:"right"`
				Bottom int `json:"bottom"`
			}{}

			jd := json.NewDecoder(req.Body)
			err := jd.Decode(cr)
			if werr := web.AsError(err, http.StatusBadRequest); werr != nil {
				return werr.AsJSON()
			}

			var img *Image
			if werr := useAPI(func(api *API) *web.Error {
				var err error
				img, err = api.GetImage(id)
				return web.AsError(err, http.StatusInternalServerError)
			}); werr != nil {
				return werr.AsJSON()
			}
			if img == nil {
				return web.AsError(fmt.Errorf("Could not find image by ID"), http.StatusNotFound).AsJSON()
			}

			// Crop the image:
			// _, ext, thumbExt := imageKindTo(img.Kind)
			_, ext, _ := imageKindTo(img.Kind)
			local_path := path.Join(store_folder(), strconv.FormatInt(img.ID, 10)+ext)
			tmp_output, err := cropImage(local_path, cr.Left, cr.Top, cr.Right, cr.Bottom)
			if werr := web.AsError(err, http.StatusInternalServerError); werr != nil {
				return werr.AsJSON()
			}

			// Clone the image record to a new record:
			if werr := useAPI(func(api *API) *web.Error {
				var err error
				img.ID = 0
				img.ID, err = api.NewImage(img)
				return web.AsError(err, http.StatusInternalServerError)
			}); werr != nil {
				return werr.AsJSON()
			}

			// Move the temp file to the final storage path:
			img_name := strconv.FormatInt(img.ID, 10)
			os.MkdirAll(store_folder(), 0755)
			store_path := path.Join(store_folder(), img_name+ext)
			if werr := web.AsError(os.Rename(tmp_output, store_path), http.StatusInternalServerError); werr != nil {
				return werr.AsJSON()
			}

			//// Generate a thumbnail:
			//os.MkdirAll(thumb_folder(), 0755)
			//thumb_path := path.Join(thumb_folder(), img_name+thumbExt)
			//if web.AsError(generateThumbnail(firstImage, newImage.Kind, thumb_path), http.StatusInternalServerError).AsJSON().Respond(rsp) {
			//	return
			//}
			width, height := cr.Right-cr.Left, cr.Bottom-cr.Top

			web.JsonSuccess(rsp, &struct {
				ID             int64  `json:"id"`
				Base62ID       string `json:"base62id"`
				Title          string `json:"title"`
				CollectionName string `json:"collectionName,omitempty"`
				Submitter      string `json:"submitter,omitempty"`
				Kind           string `json:"kind"`
				Width          *int   `json:"width,omitempty"`
				Height         *int   `json:"height,omitempty"`
			}{
				ID:             img.ID,
				Base62ID:       b62.Encode(img.ID + 10000),
				Kind:           img.Kind,
				Title:          img.Title,
				CollectionName: img.CollectionName,
				Submitter:      img.Submitter,
				Width:          &width,
				Height:         &height,
			})
			return nil
		}

		rsp.WriteHeader(http.StatusBadRequest)
		return nil
	}

	// GET:
	req_query := req.URL.Query()
	nsfw := false
	nsfw_s := req_query.Get("nsfw")
	if nsfw_s != "" {
		nsfw = true
	}

	var orderBy ImagesOrderBy
	if _, ok := req_query["title"]; ok {
		orderBy = ImagesOrderByTitleASC
	} else if _, ok := req_query["oldest"]; ok {
		orderBy = ImagesOrderByIDASC
	} else {
		orderBy = ImagesOrderByIDDESC
	}

	if req.URL.Path == "/favicon.ico" {
		return web.NewError(nil, http.StatusNoContent, web.Empty)
	} else if req.URL.Path == "/" {
		keywords := normalizeKeywords(req_query["q"])
		list, werr := apiSearch(keywords, "all", true, orderBy)
		if werr != nil {
			return werr.AsHTML()
		}

		listCollection(rsp, req, keywords, "", list, nsfw)
		return nil
	} else if collectionName, ok := web.MatchSimpleRoute(req.URL.Path, "/col/list"); ok {
		keywords := normalizeKeywords(req_query["q"])
		list, werr := apiSearch(keywords, collectionName, true, orderBy)
		if werr != nil {
			return werr.AsHTML()
		}

		listCollection(rsp, req, keywords, collectionName, list, nsfw)
		return nil
	} else if collectionName, ok := web.MatchSimpleRoute(req.URL.Path, "/col/only"); ok {
		keywords := normalizeKeywords(req_query["q"])
		list, werr := apiSearch(keywords, collectionName, false, orderBy)
		if werr != nil {
			return werr.AsHTML()
		}

		listCollection(rsp, req, keywords, collectionName, list, nsfw)
		return nil
	} else if collectionName, ok := web.MatchSimpleRoute(req.URL.Path, "/col/add"); ok {
		model := &struct {
			AddURL    string
			UploadURL string
		}{}
		model.AddURL = "/col/add"
		model.UploadURL = "/col/upload"
		if collectionName != "" {
			model.AddURL = "/col/add/" + collectionName
			model.UploadURL = "/col/upload/" + collectionName
		}

		// GET the /col/add form to add a new image:
		rsp.Header().Set("Content-Type", "text/html; charset=utf-8")
		rsp.WriteHeader(200)
		if werr := web.AsError(uiTmpl.ExecuteTemplate(rsp, "new", model), http.StatusInternalServerError); werr != nil {
			return werr.AsHTML()
		}
		return nil
	} else if web.MatchExactRouteIgnoreSlash(req.URL.Path, "/admin") {
		list, werr := getList("all", true, orderBy)
		if werr != nil {
			return werr.AsHTML()
		}

		// Project into a view model:
		model := struct {
			List []ImageViewModel
		}{
			List: projectModelList(list),
		}

		// GET the /admin/list to link to edit pages:
		rsp.Header().Set("Content-Type", "text/html; charset=utf-8")
		rsp.WriteHeader(200)
		if werr := web.AsError(uiTmpl.ExecuteTemplate(rsp, "admin", model), http.StatusInternalServerError); werr != nil {
			return werr.AsHTML()
		}
		return nil
	} else if collectionName, ok := web.MatchSimpleRoute(req.URL.Path, "/admin/list"); ok {
		list, werr := getList(collectionName, true, orderBy)
		if werr != nil {
			return werr.AsHTML()
		}

		// Project into a view model:
		model := struct {
			List []ImageViewModel
		}{
			List: projectModelList(list),
		}

		// GET the /admin/list to link to edit pages:
		rsp.Header().Set("Content-Type", "text/html; charset=utf-8")
		rsp.WriteHeader(200)
		if werr := web.AsError(uiTmpl.ExecuteTemplate(rsp, "admin", model), http.StatusInternalServerError); werr != nil {
			return werr.AsHTML()
		}
		return nil
	} else if id_s, ok := web.MatchSimpleRoute(req.URL.Path, "/admin/edit"); ok {
		id := b62.Decode(id_s) - 10000

		var img *Image
		if werr := useAPI(func(api *API) *web.Error {
			var err error
			img, err = api.GetImage(id)
			return web.AsError(err, http.StatusInternalServerError)
		}); werr != nil {
			return werr.AsHTML()
		}
		if img == nil {
			return web.AsError(fmt.Errorf("Could not find image by ID"), http.StatusNotFound).AsHTML()
		}

		// Project into a view model:
		model := viewTemplateModel{
			BGColor: "gray",
			Query:   flattenQuery(req_query),
			Image:   *xlatImageViewModel(img, nil),
			// Allow editing:
			IsAdmin: true,
		}

		// GET the /admin/list to link to edit pages:
		rsp.Header().Set("Content-Type", "text/html; charset=utf-8")
		rsp.WriteHeader(200)
		if werr := web.AsError(uiTmpl.ExecuteTemplate(rsp, "view", model), http.StatusInternalServerError); werr != nil {
			return werr.AsHTML()
		}
		return nil
	} else if v_id, ok := web.MatchSimpleRoute(req.URL.Path, "/view/yt"); ok {
		// GET /view/yt/<video_id> to display a youtube player page for <video_id>, e.g. `dQw4w9WgXcQ`
		model := viewTemplateModel{
			BGColor:    "black",
			FillScreen: true,
			Query:      flattenQuery(req_query),
			Image: *xlatImageViewModel(&Image{
				ID:             int64(0),
				Kind:           "youtube",
				Title:          v_id,
				SourceURL:      &v_id,
				CollectionName: "",
				Submitter:      "",
				RedirectToID:   nil,
				IsHidden:       true,
				IsClean:        false,
				Keywords:       "",
			}, nil),
		}

		// Set controls=1 if it's missing:
		if _, ok := model.Query["controls"]; !ok {
			model.Query["controls"] = "1"
		}

		rsp.Header().Set("Content-Type", "text/html; charset=utf-8")
		if werr := web.AsError(uiTmpl.ExecuteTemplate(rsp, "view", model), http.StatusInternalServerError); werr != nil {
			return werr.AsHTML()
		}
		return nil
	} else if imgurl, ok := web.MatchSimpleRouteRaw(req.URL.Path, "/view/img/"); ok {
		// GET /view/img/<imgurl> to display an image viewer page for any URL <imgurl>, e.g. `//`
		model := viewTemplateModel{
			BGColor: "black",
			Query:   flattenQuery(req_query),
			Image: ImageViewModel{
				ID:             int64(0),
				Base62ID:       "_",
				Title:          imgurl,
				Kind:           "jpeg",
				ImageURL:       imgurl,
				ThumbURL:       "",
				Submitter:      "",
				CollectionName: "",
				SourceURL:      &imgurl,
				RedirectToID:   nil,
				IsClean:        false,
				Keywords:       "",
			},
		}

		rsp.Header().Set("Content-Type", "text/html; charset=utf-8")
		if werr := web.AsError(uiTmpl.ExecuteTemplate(rsp, "view", model), http.StatusInternalServerError); werr != nil {
			return werr.AsHTML()
		}
		return nil
	} else if collectionName, ok := web.MatchSimpleRoute(req.URL.Path, "/api/v1/list"); ok {
		// `/api/v1/list/all` returns all images across all collections.
		list, werr := getList(collectionName, true, orderBy)
		return apiListResult(req, rsp, list, werr)
	} else if collectionName, ok := web.MatchSimpleRoute(req.URL.Path, "/api/v1/only"); ok {
		list, werr := getList(collectionName, false, orderBy)
		return apiListResult(req, rsp, list, werr)
	} else if collectionName, ok := web.MatchSimpleRoute(req.URL.Path, "/api/v1/search"); ok {
		// Join and resplit keywords by spaces because `req_query["q"]` splits at `q=1&q=2&q=3` level, not spaces.
		keywords := normalizeKeywords(req_query["q"])
		list, werr := apiSearch(keywords, collectionName, true, orderBy)
		return apiListResult(req, rsp, list, werr)
	} else if id_s, ok := web.MatchSimpleRoute(req.URL.Path, "/api/v1/info"); ok {
		id := b62.Decode(id_s) - 10000

		var img *Image
		if werr := useAPI(func(api *API) *web.Error {
			var err error
			img, err = api.GetImage(id)
			return web.AsError(err, http.StatusInternalServerError)
		}); werr != nil {
			return werr.AsJSON()
		}
		if img == nil {
			return web.AsError(fmt.Errorf("Could not find image by ID"), http.StatusNotFound).AsJSON()
		}

		// Decode the image and grab its properties:
		_, ext, _ := imageKindTo(img.Kind)
		local_path := path.Join(store_folder(), strconv.FormatInt(img.ID, 10)+ext)

		model := &struct {
			ID             int64   `json:"id"`
			Base62ID       string  `json:"base62id"`
			Title          string  `json:"title"`
			Keywords       string  `json:"keywords"`
			CollectionName string  `json:"collectionName,omitempty"`
			Submitter      string  `json:"submitter,omitempty"`
			Kind           string  `json:"kind"`
			SourceURL      *string `json:"sourceURL,omitempty"`
			RedirectToID   *int64  `json:"redirectToID,omitempty"`
			Width          *int    `json:"width,omitempty"`
			Height         *int    `json:"height,omitempty"`
		}{
			ID:             id,
			Base62ID:       b62.Encode(id + 10000),
			Kind:           img.Kind,
			Title:          img.Title,
			Keywords:       img.Keywords,
			CollectionName: img.CollectionName,
			Submitter:      img.Submitter,
			SourceURL:      img.SourceURL,
			RedirectToID:   img.RedirectToID,
		}
		if model.Kind == "" {
			model.Kind = "gif"
		}

		if model.Kind != "youtube" {
			var width, height int
			var err error

			width, height, model.Kind, err = getImageInfo(local_path)
			if werr := web.AsError(err, http.StatusInternalServerError); werr != nil {
				return werr.AsJSON()
			}

			model.Width = &width
			model.Height = &height
		}

		web.JsonSuccess(rsp, model)
		return nil
	}

	dir := path.Dir(req.URL.Path)

	// Look up the image's record by base62 encoded ID:
	filename := path.Base(req.URL.Path)
	filename = filename[0 : len(filename)-len(path.Ext(req.URL.Path))]

	id := b62.Decode(filename) - 10000

	var img *Image
	var err error
	if werr := useAPI(func(api *API) *web.Error {
		img, err = api.GetImage(id)
		if err != nil {
			return web.AsError(err, http.StatusInternalServerError)
		}
		if img == nil {
			return web.AsError(fmt.Errorf("No record for ID exists"), http.StatusNotFound)
		}

		// Follow redirect chain:
		for img.RedirectToID != nil {
			newimg, err := api.GetImage(*img.RedirectToID)
			if err != nil {
				return web.AsError(err, http.StatusInternalServerError)
			}
			img = newimg
		}

		return nil
	}); werr != nil {
		return werr.AsHTML()
	}

	// Determine mime-type and file extension:
	if img.Kind == "" {
		img.Kind = "gif"
	}
	mime, ext, thumbExt := imageKindTo(img.Kind)

	// Find the image file:
	img_name := strconv.FormatInt(img.ID, 10)

	if dir == "/b" || dir == "/w" || dir == "/g" {
		// Render a black or white BG centered image viewer:
		var bgcolor string
		switch dir {
		case "/b":
			bgcolor = "black"
		case "/w":
			bgcolor = "white"
		case "/g":
			bgcolor = "gray"
		}

		model := viewTemplateModel{
			BGColor: bgcolor,
			Query:   flattenQuery(req_query),
			Image:   *xlatImageViewModel(img, nil),
		}

		rsp.Header().Set("Content-Type", "text/html; charset=utf-8")
		if werr := web.AsError(uiTmpl.ExecuteTemplate(rsp, "view", model), http.StatusInternalServerError); werr != nil {
			return werr.AsHTML()
		}

		return nil
	} else if dir == "/t" {
		// Serve thumbnail file:
		local_path := path.Join(store_folder(), img_name+ext)
		thumb_path := path.Join(thumb_folder(), img_name+thumbExt)
		if werr := web.AsError(ensureThumbnail(local_path, thumb_path), http.StatusInternalServerError); werr != nil {
			runtime.GC()
			return werr.AsHTML()
		}

		if xrThumb != "" {
			// Pass request to nginx to serve static content file:
			redirPath := path.Join(xrThumb, img_name+thumbExt)

			rsp.Header().Set("X-Accel-Redirect", redirPath)
			rsp.Header().Set("Content-Type", mime)
			rsp.WriteHeader(200)
			runtime.GC()
			return nil
		} else {
			rsp.Header().Set("Content-Type", mime)
			http.ServeFile(rsp, req, thumb_path)
			runtime.GC()
			return nil
		}
	}

	// Serve actual image contents:
	if xrGif != "" {
		// Pass request to nginx to serve static content file:
		redirPath := path.Join(xrGif, img_name+ext)

		rsp.Header().Set("X-Accel-Redirect", redirPath)
		rsp.Header().Set("Content-Type", mime)
		rsp.WriteHeader(200)
		runtime.GC()
		return nil
	} else {
		// Serve content directly with the proper mime-type:
		local_path := path.Join(store_folder(), img_name+ext)

		rsp.Header().Set("Content-Type", mime)
		http.ServeFile(rsp, req, local_path)
		runtime.GC()
		return nil
	}
}

// Unused. For upgrade purposes.
// Set all Keywords for images:
func UpdateKeywords() {
	api, err := NewAPI()
	if err != nil {
		panic(err)
	}
	defer api.Close()

	imgs, err := api.GetList("all", true, ImagesOrderByIDASC)
	for _, img := range imgs {
		if img.Keywords != "" {
			continue
		}

		keywords := splitToWords(strings.ToLower(img.Title))
		//log.Printf("%+v\n", keywords)

		img.Keywords = strings.Join(keywords, " ")

		err = api.Update(&img)
		if err != nil {
			//log.Println(err)
		}
	}
}
