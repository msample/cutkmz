package cmd

import (
	"archive/zip"
	"bytes"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"

	"github.com/golang/glog"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

const (
	north int = iota // index in box []float64 for its dec. degrees
	south
	east
	west

	convProg     = "convert"  // img mgck. "gm convert" poss
	identifyProg = "identify" // "gm identify" ditto
)

const kmlHdrTmpl = `<?xml version="1.0" encoding="UTF-8"?>
<kml xmlns="http://www.opengis.net/kml/2.2">
<Document>
  <name>{{ .Name }}</name>
`

const kmlOverlayTmpl = `  <GroundOverlay>
    <name>{{ .Name }}</name>
    <color>bdffffff</color>
    <drawOrder>{{ .DrawingOrder }} </drawOrder>
    <Icon>
      <href>{{ .TileFileName }}</href>
      <viewBoundScale>1.0</viewBoundScale>
    </Icon>
    <LatLonBox>
      <north>{{ .North }}</north>
      <south>{{ .South }} </south>
      <east>{{  .East  }}</east>
      <west>{{  .West  }}</west>
      <rotation>0.0</rotation>
    </LatLonBox>
  </GroundOverlay>
`

const kmlFtr = `</Document>
</kml>
`

var kmzCmd = &cobra.Command{
	Use:   "kmz",
	Short: "Creates .kmz from a JPG with map tiles small enough for a Garmin GPS",
	Long: `Creates .kmz map tiles for a Garmin from a larger geo-poisitioned map image. Tested on a 62s & 64s

Crunches and converts a raster image (.jpg,.gif,.tiff etc) to match what Garmin devices can handle wrt resolution and max tile-size.

Rather than expect metadata files with geo-positioning information for
the jpg, cutkmz expects it to be encoded into the file's name,
"name-geo-anchored". Harder to lose. For example:

    Grouse-Mountain_49.336694_49.470628_-123.132056_-122.9811.jpg

Underscores are required: <map-name>_<North-lat>_<South-lat>_<East-long>_<West-long>.<fmt>

Garmin limits the max tiles per model (100 on 62s, 500 on Montana,
Oregon 600 series and GPSMAP 64 series. Tiles of more than 1 megapixel
(w*h) add no additional clarity. If you have a large image, it will be
reduced in quality until it can be chopped in max-tiles or less
1024x1024 chunks. 

Connect your GPS via USB and copy the generated kmz files into /Garmin/CustomMap (SD or main mem).

Garmin limitations on .kmz files and the images in them:
  * image must be jpeg, not 'progressive'
  * only considers the /doc.kml in the .kmz
  * tiles over 1MP, e.g. > 1024x1024 or 512x2048 etc pixels do not add increased resolution
  * each tile jpeg should be less than 3MB.
  * Max images/tiles per device: typically 100. 500 on some.
  * smaller image files are rendered faster

Requires the imagemagick to be installed on your system, and uses its
"convert" and "identify" programs

`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("DIRS", north, south, east, west)
		if err := process(viper.GetViper(), args); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			fmt.Fprintf(os.Stderr, "see 'cutkmz kmz -h' for help\n")
			os.Exit(1)
		}
	},
}

func init() {
	RootCmd.AddCommand(kmzCmd)

	kmzCmd.Flags().StringP("image", "i", "", "image file named with its bounding box in decimal degrees.")
	viper.BindPFlag("image", kmzCmd.Flags().Lookup("image"))

	kmzCmd.Flags().IntP("max_tiles", "t", 100, "max # pieces to cut jpg into. Beware of device limits.")
	viper.BindPFlag("max_tiles", kmzCmd.Flags().Lookup("max_tiles"))

	kmzCmd.Flags().IntP("drawing_order", "d", 51, "Garmins make values > 50 visible. Tune if have overlapping overlays.")
	viper.BindPFlag("drawing_order", kmzCmd.Flags().Lookup("drawing_order"))

	kmzCmd.Flags().AddGoFlagSet(flag.CommandLine)
	flag.CommandLine.VisitAll(func(f *flag.Flag) {
		viper.BindPFlag(f.Name, kmzCmd.Flags().Lookup(f.Name))
	})
	flag.CommandLine.Parse(nil) // shut up 'not parsed' complaints
}

//  getBox returns map name & lat/long bounding box by extracing it
//  from the name. The Float slice is in order: northLat, southLat,
//  eastLong, westLong in decimal degrees
func getBox(image string) (base string, box []float64, err error) {
	c := strings.Split(image, "_")
	if len(c) != 5 {
		err = fmt.Errorf("File name must include bounding box name_N_S_E_W.jpg in decimal degrees, e.g. Grouse-Mountain_49.336694_49.470628_-123.132056_-122.9811.jpg")
		return
	}
	base = filepath.Base(c[0])
	for i := 1; i < 5; i++ {
		if i == 4 {
			s := strings.SplitN(c[i], ".", 3)
			if len(s) == 3 {
				c[i] = s[0] + "." + s[1]
			}
		}
		f, err := strconv.ParseFloat(c[i], 64)
		if err != nil {
			err = fmt.Errorf("Error parsing lat/long degrees in file name: %v", err)
			return "", nil, err
		}
		box = append(box, f)
	}
	return
}

// imageWxH returns the width and height of image file in pixels
func imageWxH(imageFilename string) (width int, height int, err error) {
	cmd := exec.Command(identifyProg, "-format", "%w %h", imageFilename)
	glog.Infof("About to run: %#v\n", cmd.Args)
	var b []byte
	b, err = cmd.Output()
	if err != nil {
		return 0, 0, err
	}
	wh := bytes.Split(b, []byte(" "))
	if len(wh) != 2 {
		return 0, 0, fmt.Errorf("Expected two ints separated by space, but got: %v", b)
	}
	width, err = strconv.Atoi(string(wh[0]))
	if err != nil {
		return
	}
	height, err = strconv.Atoi(string(wh[0]))
	if err != nil {
		return
	}
	return
}

// process the name-geo-anchored files args into KMZs
func process(v *viper.Viper, args []string) error {
	maxTiles := v.GetInt("max_tiles")
	drawingOrder := v.GetInt("drawing_order")

	if len(args) == 0 {
		glog.Error("Image file required")
		return fmt.Errorf("must specify an imaage file")
	}

	for _, image := range args {
		absImage, err := filepath.Abs(image)
		if err != nil {
			glog.Errorf("Issue with an image file path: %v", err)
			return err
		}
		base, box, err := getBox(absImage)
		if err != nil {
			glog.Errorf("Error with image file name: %v", err)
			return err
		}
		width, height, err := imageWxH(absImage)
		if err != nil {
			glog.Errorf("Error extracting image dimensions: %v", err)
			return err
		}

		maxPixels := maxTiles * 1024 * 1024
		tmpDir, err := ioutil.TempDir("", "cutkmz-")
		if err != nil {
			glog.Errorf("Error creating a temporary directory: %v", err)
			return err
		}
		tilesDir := filepath.Join(tmpDir, base, "tiles")
		err = os.MkdirAll(tilesDir, 0755)
		if err != nil {
			glog.Errorf("Error creating making tiles tmp dir: %v", err)
			return err
		}

		if maxPixels < (height * width) {
			resizeFixToJpgs(absImage, tilesDir, base, maxPixels)
		} else {
			fixToJpgs(absImage, tilesDir, base)
		}

		/*
			wrkFile := absImage
			var nextWrkFile string
			if maxPixels < (height * width) {
				if wrkFile, err = tmpFileName(".jpg"); err != nil {
					return err
				}
				if err = resizeImage(absImage, wrkFile, maxPixels); err != nil {
					return err
				}
			}

			if nextWrkFile, err = tmpFileName(".jpg"); err != nil {
				return err
			}
			if err = fixToJpg(wrkFile, nextWrkFile); err != nil {
				return err
			}
			wrkFile = nextWrkFile

			width, height, err = imageWxH(absImage)
			if err != nil {
				glog.Errorf("Error extracting image dimensions from fixed image: %v", err)
				return err
			}
			if err = chopJpg(wrkFile, tilesDir, base); err != nil {
				return err
			}
		*/

		var twtr *os.File

		if twtr, err = os.Create(filepath.Join(tmpDir, base, "doc.kml")); err != nil {
			return err
		}
		if err = startKML(twtr, base); err != nil {
			return err
		}

		// for each jpg tile create an entry in the kml file with is bounding box
		var tileFiles []os.FileInfo
		if tileFiles, err = ioutil.ReadDir(tilesDir); err != nil {
			return err
		}
		var widthSum int
		var tbox = []float64{0, 0, 0, 0}
		tbox[north] = box[north]
		tbox[west] = box[west]
		for i, tf := range tileFiles {
			var tw, th int
			if tw, th, err = imageWxH(filepath.Join(tilesDir, tf.Name())); err != nil {
				return err
			}
			nsDeltaDeg := (float64(th) / float64(height)) * (box[north] - box[south])
			ewDeltaDeg := (float64(tw) / float64(width)) * math.Mod(box[east]-box[west], 360)
			widthSum += tw
			tbox[east] = tbox[west] - ewDeltaDeg
			tbox[south] = tbox[north] - nsDeltaDeg
			// tile file relative path to doc.kml
			tileFileOnly := base + fmt.Sprintf("_%03d", i)
			tFile := filepath.Join("tiles", tileFileOnly)
			if err = KMLAddOverlay(twtr, tileFileOnly, tbox, drawingOrder, tFile); err != nil {
				return err
			}

			if widthSum >= width {
				// drop down a row
				tbox[north] = tbox[south]
				tbox[west] = box[west]
				widthSum = 0
			} else {
				tbox[west] = tbox[east]
			}
		}
		endKML(twtr)
		twtr.Close()
		var zf *os.File
		if zf, err = os.Create(base + ".kmz"); err != nil {
			return err
		}
		zipd(filepath.Join(tmpDir, base), zf)
	}
	return nil
}

func startKML(w io.Writer, name string) error {
	t, err := template.New("kmlhdr").Parse(kmlHdrTmpl)
	if err != nil {
		return err
	}
	root := struct{ Name string }{name}
	return t.Execute(w, &root)
}

func KMLAddOverlay(w io.Writer, tileName string, tbox []float64, drawingOrder int, relTileFile string) error {
	t, err := template.New("kmloverlay").Parse(kmlOverlayTmpl)
	if err != nil {
		return err
	}
	root := struct {
		Name         string
		TileFileName string
		DrawingOrder int
		North        float64
		South        float64
		East         float64
		West         float64
	}{tileName, relTileFile, drawingOrder, tbox[north], tbox[south], tbox[east], tbox[west]}
	return t.Execute(w, &root)
}

func endKML(w io.Writer) error {
	t, err := template.New("kmlftr").Parse(kmlFtr)
	if err != nil {
		return err
	}
	return t.Execute(w, nil)
}

func resizeFixToJpgs(inFile, outDir, baseName string, maxPixArea int) error {
	outFile := filepath.Join(outDir, baseName+"_tile_%03d.jpg")
	// param order super sensitive
	cmd := exec.Command("convert", "-resize", "@"+fmt.Sprintf("%v", maxPixArea), "-crop", "1024x1024", inFile, "-strip", "-interlace", "none", "+adjoin", outFile)
	glog.Infof("About to run: %#v\n", cmd.Args)
	_, err := cmd.Output()
	if err != nil {
		return err
	}
	return nil
}

func fixToJpgs(inFile, outDir, baseName string) error {
	outFile := filepath.Join(outDir, baseName+"_tile_%03d.jpg")
	cmd := exec.Command("convert", "-crop", "1024x1024", inFile, "-strip", "-interlace", "none", "+adjoin", outFile)
	glog.Infof("About to run: %#v\n", cmd.Args)
	_, err := cmd.Output()
	if err != nil {
		return err
	}
	return nil
}

// zipd makes a zip archive of the given dirctory and writes it to the
// writer. Paths in the zip archive are relative to the base name of
// the given directory.
func zipd(dir string, w io.Writer) error {
	z := zip.NewWriter(w)
	defer func() {
		if err := z.Flush(); err != nil {
			fmt.Printf("Error flushing ZIP writer: %v\n", err)
		}
		if err := z.Close(); err != nil {
			fmt.Printf("Error closing ZIP writer: %v\n", err)
		}
	}()
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		r, err := os.Open(path)
		if err != nil {
			return err
		}
		defer r.Close()
		zw, err := z.Create(rel)
		if err != nil {
			return err
		}
		_, err = io.Copy(zw, r)
		if err != nil {
			return err
		}
		return nil
	})

	return nil
}

/*
func tmpFile(suffix string) (*os.File, error) {
	f, err := ioutil.TempFile("", "cutkmz-")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	newName := f.Name() + suffix
	err = os.Rename(f.Name(), newName)
	if err != nil {
		return nil, err
	}
	return os.OpenFile(newName, os.O_RDWR, 0)
}

func tmpFileName(suffix string) (string, error) {
	f, err := tmpFile(suffix)
	if err != nil {
		return "", err
	}
	name := f.Name()
	f.Close()
	return name, nil
}

// resizeImage converts inFile image to one that has a maximum pixel
// area (width x height) of maxPixArea. Result is written to
// outfile. Suffix on outFile name is used to deterime raster format
// of the result (.jpg, .tiff, etc)
func resizeImage(inFile, outFile string, maxPixArea int) error {
	cmd := exec.Command(convProg, inFile, "-resize", "@"+strconv.Itoa(maxPixArea), outFile)
	fmt.Printf("About to run: %v\n", cmd)
	_, err := cmd.Output()
	if err != nil {
		return err
	}
	return nil
}

// fixToJpg strips unnecessary extra information (profiles, comments)
// from the source raster image (.jpg, .tiff, etc) and produces a
// non-interlaced .jpg as the result
func fixToJpg(inFile, outFile string) error {
	cmd := exec.Command(convProg, inFile, "-strip", "-interlace", "none", outFile)
	fmt.Printf("About to run: %v\n", cmd)
	_, err := cmd.Output()
	if err != nil {
		return err
	}
	return nil
}

// chopJpg chops inFile jpg into itty bitty tiles of 1024x1024 and
// writes them into given directory. Parts are named baseName_tile_%03d.jpg"
// FIXME: document which parts are less than 1024x1024 right edge and bottom?
func chopJpg(inFile, outDir, baseName string) error {
	cmd := exec.Command(convProg, "-crop", "1024x1024", inFile, "+adjoin", filepath.Join(outDir, baseName+"_tile_%03d.jpg"))
	fmt.Printf("About to run: %v\n", cmd)
	_, err := cmd.Output()
	if err != nil {
		return err
	}
	return nil
}
*/
