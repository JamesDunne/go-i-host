package main

import (
	"fmt"
	"io"
	//"log"
	"os"
	//"path"
	"reflect"
)

import (
	"github.com/JamesDunne/go-util/imaging/gif" // my own patches to image/gif
	"image"
	"image/jpeg"
	"image/png"
)

import "github.com/JamesDunne/go-util/imaging"

func makeThumbnail(img image.Image, dimensions int) (thumbImg image.Image) {
	// Calculate the largest square bounds for a thumbnail to preserve aspect ratio
	b := img.Bounds()
	srcBounds := b
	dx, dy := b.Dx(), b.Dy()
	if dx > dy {
		offs := (dx - dy) / 2
		srcBounds.Min.X += offs
		srcBounds.Max.X -= offs
	} else if dy > dx {
		offs := (dy - dx) / 2
		srcBounds.Min.Y += offs
		srcBounds.Max.Y -= offs
	} else {
		// Already square.
	}

	//log.Printf("'%s': resize %v to %v\n", filename, b, srcBounds)

	// Cut out the center square to a new image:
	var boximg image.Image
	switch si := img.(type) {
	case *image.RGBA:
		boximg = si.SubImage(srcBounds)
	case *image.YCbCr:
		boximg = si.SubImage(srcBounds)
	case *image.Paletted:
		boximg = si.SubImage(srcBounds)
	case *image.RGBA64:
		panic(fmt.Errorf("Unhandled image format type: %s", "RGBA64"))
	case *image.NRGBA:
		boximg = si.SubImage(srcBounds)
	case *image.NRGBA64:
		panic(fmt.Errorf("Unhandled image format type: %s", "NRGBA64"))
	case *image.Alpha:
		panic(fmt.Errorf("Unhandled image format type: %s", "Alpha"))
	case *image.Alpha16:
		panic(fmt.Errorf("Unhandled image format type: %s", "Alpha16"))
	case *image.Gray:
		boximg = si.SubImage(srcBounds)
	case *image.Gray16:
		panic(fmt.Errorf("Unhandled image format type: %s", "Gray16"))
	default:
		panic(fmt.Errorf("Unhandled image format type: %s", reflect.TypeOf(img).Name()))
	}

	img = nil

	// Apply resizing algorithm:
	thumbImg = imaging.Resize(boximg, dimensions, dimensions, imaging.Lanczos)

	boximg = nil

	return
}

func generateThumbnail(firstImage image.Image, imageKind string, thumb_path string) error {
	if firstImage == nil {
		return fmt.Errorf("Cannot generate thumbnail with nil image")
	}

	var encoder func(w io.Writer, m image.Image) error
	switch imageKind {
	case "jpeg":
		encoder = func(w io.Writer, img image.Image) error { return jpeg.Encode(w, img, &jpeg.Options{Quality: 100}) }
	case "png":
		encoder = png.Encode
	case "gif":
		encoder = png.Encode
	}

	// Generate the thumbnail image:
	thumbImg := makeThumbnail(firstImage, thumbnail_dimensions)
	defer func() {
		firstImage = nil
		thumbImg = nil
	}()

	// Save it to a file:
	os.Remove(thumb_path)
	tf, err := os.Create(thumb_path)
	if err != nil {
		return err
	}
	defer tf.Close()

	// Write the thumbnail to the file:
	err = encoder(tf, thumbImg)
	if err != nil {
		return err
	}

	return nil
}

func decodeFirstImage(local_path string) (firstImage image.Image, imageKind string, err error) {
	imf, err := os.Open(local_path)
	if err != nil {
		return nil, "", err
	}
	defer imf.Close()

	_, imageKind, err = image.DecodeConfig(imf)
	if err != nil {
		return nil, "", err
	}
	imf.Seek(0, 0)

	switch imageKind {
	case "gif":
		// Decode GIF frames until we reach the first fully opaque image:
		var g *gif.GIF
		g, err = gif.DecodeAll(imf)
		if err != nil {
			return nil, "", err
		}

		firstFrame := g.Image[0]
		g.Image = nil
		g.Delay = nil
		g = nil

		return firstFrame, imageKind, nil
	default:
		firstImage, imageKind, err = image.Decode(imf)
		if err != nil {
			return nil, "", err
		}
		return
	}
}
