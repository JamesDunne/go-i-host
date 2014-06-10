package main

import (
	"fmt"
	"image"
	"reflect"
)
import "github.com/JamesDunne/go-util/imaging/resize"

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

	//log.Printf("'%s': resized to %v\n", filename, boximg.Bounds())

	// Apply resizing algorithm:
	thumbImg = resize.Resize(boximg, boximg.Bounds(), dimensions, dimensions)

	return
}
