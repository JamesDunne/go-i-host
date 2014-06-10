package main

import (
	"fmt"
	"image"
	"log"
	"reflect"
)
import "github.com/JamesDunne/go-util/imaging/resize"

func makeThumbnail(img image.Image, dimensions int) (thumbImg image.Image) {
	// Calculate the largest square bounds for a thumbnail to preserve aspect ratio
	b := img.Bounds()
	srcBounds := b
	dx, dy := b.Dx(), b.Dy()
	if dx > dy {
		offs := (dy / 2)
		srcBounds.Min.X = (dx / 2) - offs
		srcBounds.Max.X = (dx / 2) + offs
	} else if dy > dx {
		offs := (dx / 2)
		srcBounds.Min.Y = (dy / 2) - offs
		srcBounds.Max.Y = (dy / 2) + offs
	} else {
		// Already square.
	}

	//log.Printf("resize %v to %v\n", b, srcBounds)

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

	sb := boximg.Bounds()
	hackedSB := image.Rectangle{image.Pt(sb.Min.X, 0), image.Pt(sb.Max.X, sb.Dy())}
	log.Printf("resizing to %v, really %v\n", sb, hackedSB)

	// Apply resizing algorithm:
	thumbImg = resize.Resize(boximg, hackedSB, dimensions, dimensions)

	return
}
