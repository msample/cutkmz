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

const (
	north int = iota // index into [4]float64 assoc dec. degrees
	south
	east
	west
)

// mapTile holds an image filepath, its lat/long bounding box and
// pixel width & height
type mapTile struct {
	fpath  string     // file path of tile image
	width  int        // Tile width in pixels
	height int        // Tile height in pixels
	box    [4]float64 // lat&long bounding box in decimal degrees
}

// NewMapTile populates a map tile using the given width and height
// instead of extracting it from the given file path. Panics if North
// < South or cross a pole.
func NewMapTile(fpath string, pixWid, pixHigh int, n, s, e, w float64) *mapTile {
	if n > 90 || s < -90 || n < s {
		panic("No crossing a pole and map's North must be greater than South")
	}
	rv := &mapTile{
		fpath:  fpath,
		width:  pixWid,
		height: pixHigh,
		box:    [4]float64{n, s, normEasting(e), normEasting(w)},
	}
	return rv
}

// NewMapTileFromFile reads in given file path and creates a map tile
// with the filepath and pix width & height from the image.
func NewMapTileFromFile(fpath string, n, s, e, w float64) (*mapTile, error) {
	wid, high, err := imageWxH(fpath)
	if err != nil {
		return nil, err
	}
	return NewMapTile(fpath, wid, high, n, s, e, w), nil
}

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

	kmzCmd.Flags().BoolP("keep_tmp", "k", false, "Don't delete intermediate files from $TMPDIR.")
	viper.BindPFlag("keep_tmp", kmzCmd.Flags().Lookup("keep_tmp"))

	kmzCmd.Flags().AddGoFlagSet(flag.CommandLine)
	flag.CommandLine.VisitAll(func(f *flag.Flag) {
		viper.BindPFlag(f.Name, kmzCmd.Flags().Lookup(f.Name))
	})
	flag.CommandLine.Parse(nil) // shut up 'not parsed' complaints
}

//  getBox returns map name & lat/long bounding box by extracing it
//  from the given file name. The Float slice is in order: northLat,
//  southLat, eastLong, westLong in decimal degrees
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

	if box[north] <= box[south] || box[north] > 90 || box[south] < -90 {
		return base, box, fmt.Errorf("North boundary must be greater than south boundary and in [-90,90]")
	}
	return
}

// imageWxH returns the width and height of image file in pixels
func imageWxH(imageFilename string) (width int, height int, err error) {
	if _, err := os.Stat(imageFilename); os.IsNotExist(err) {
		return 0, 0, err
	}
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
	height, err = strconv.Atoi(string(wh[1]))
	if err != nil {
		return
	}
	return
}

// process the name-geo-anchored files args into KMZs. Uses
// "max_tiles" and and "drawing_order" from viper if present.
func process(v *viper.Viper, args []string) error {
	maxTiles := v.GetInt("max_tiles")
	drawingOrder := v.GetInt("drawing_order")
	keepTmp := v.GetBool("keep_tmp")

	fmt.Printf("maxTiles %v, drawingOrder: %v, keepTmp: %v\n", maxTiles, drawingOrder, keepTmp)

	if len(args) == 0 {
		return fmt.Errorf("Image file required: must provide one or more imaage file path")
	}

	for _, image := range args {
		if _, err := os.Stat(image); os.IsNotExist(err) {
			return err
		}
		absImage, err := filepath.Abs(image)
		if err != nil {
			return fmt.Errorf("Issue with an image file path: %v", err)
		}
		base, box, err := getBox(absImage)
		if err != nil {
			return fmt.Errorf("Error with image file name: %v", err)
		}
		origMap, err := NewMapTileFromFile(absImage, box[north], box[south], box[east], box[west])
		if err != nil {
			return fmt.Errorf("Error extracting image dimensions: %v", err)
		}
		maxPixels := maxTiles * 1024 * 1024
		tmpDir, err := ioutil.TempDir("", "cutkmz-")
		if err != nil {
			return fmt.Errorf("Error creating a temporary directory: %v", err)
		}
		tilesDir := filepath.Join(tmpDir, base, "tiles")
		err = os.MkdirAll(tilesDir, 0755)
		if err != nil {
			return fmt.Errorf("Error making tiles dir in tmp dir: %v", err)
		}

		fixedJpg := filepath.Join(tmpDir, "fixed.jpg")
		if maxPixels < (origMap.height * origMap.width) {
			resizeFixToJpg(fixedJpg, absImage, maxPixels)
		} else {
			fixToJpg(fixedJpg, absImage)
		}

		// Need to know pixel width of map from which we
		// chopped the tiles so we know which row a tile is
		// in. Knowing the tile's row allows us to set its
		// bounding box correctly.
		fixedMap, err := NewMapTileFromFile(fixedJpg, box[north], box[south], box[east], box[west])
		if err != nil {
			return err
		}

		// chop chop chop. bork. bork bork.
		chopToJpgs(fixedJpg, tilesDir, base)

		var kdocWtr *os.File

		if kdocWtr, err = os.Create(filepath.Join(tmpDir, base, "doc.kml")); err != nil {
			return err
		}
		if err = startKML(kdocWtr, base); err != nil {
			return err
		}

		// For each jpg tile create an entry in the kml file
		// with its bounding box. Imagemagick crop+adjoin
		// chopped & numbered the tile image files
		// lexocographically ascending starting from top left
		// (000) (NW) eastwards & then down to bottom right
		// (SE). ReadDir gives sorted result.
		var tileFiles []os.FileInfo
		if tileFiles, err = ioutil.ReadDir(tilesDir); err != nil {
			return err
		}
		var widthSum int
		currNorth := fixedMap.box[north]
		currWest := fixedMap.box[west]
		for _, tf := range tileFiles {

			tile, err := NewMapTileFromFile(filepath.Join(tilesDir, tf.Name()), currNorth, 0, 0, currWest)
			if err != nil {
				return err
			}
			// righmost tiles might be narrower, bottom
			// ones shorter so must re-compute S & E edge
			// for each tile; cannot assume all same
			// size. Also double checks assumption that
			// chopping preserves number of pixels
			finishTileBox(tile, fixedMap)

			var relTPath string // file ref inside KML must be relative to kmz root
			if relTPath, err = filepath.Rel(filepath.Join(tmpDir, base), tile.fpath); err != nil {
				return err
			}
			if err = KMLAddOverlay(kdocWtr, tf.Name(), tile.box, drawingOrder, relTPath); err != nil {
				return err
			}
			widthSum += tile.width
			if widthSum >= fixedMap.width {
				// drop down a row
				currNorth = tile.box[south]
				currWest = fixedMap.box[west]
				widthSum = 0
			} else {
				currWest = tile.box[east]
			}
		}
		endKML(kdocWtr)
		kdocWtr.Close()
		var zf *os.File
		if zf, err = os.Create(base + ".kmz"); err != nil {
			return err
		}
		zipd(filepath.Join(tmpDir, base), zf)
		zf.Close()

		if !keepTmp {
			err = os.RemoveAll(tmpDir)
			if err != nil {
				return fmt.Errorf("Error removing tmp dir & contents: %v", err)
			}
		}

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

func KMLAddOverlay(w io.Writer, tileName string, tbox [4]float64, drawingOrder int, relTileFile string) error {
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

// finishTileBox completes the tile.box by setting its east and south
// boundaries relative to its current north and west values using the
// tile pixel size reltative to the full map size.
func finishTileBox(tile, fullMap *mapTile) {
	nsDeltaDeg, ewDeltaDeg := delta(tile.width, tile.height, fullMap.box, fullMap.width, fullMap.height)
	tile.box[south] = tile.box[north] - nsDeltaDeg
	tile.box[east] = tile.box[west] + ewDeltaDeg
}

// delta returns the how many degrees further South the bottom of the
// tile is than the top, and how many degrees further east the east
// edge of the tile is than the west, given the tile width & height in
// pixels, the map's bounding box in decimal degrees, and the map's
// total width and height in pixels
func delta(tileWidth, tileHeight int, box [4]float64, totWidth, totHeight int) (nsDeltaDeg float64, ewDeltaDeg float64) {
	nsDeltaDeg = (float64(tileHeight) / float64(totHeight)) * (box[north] - box[south])
	ewDeg := eastDelta(box[east], box[west])
	ewDeltaDeg = (float64(tileWidth) / float64(totWidth)) * ewDeg
	return
}

// eastDelta returns the positve decimal degrees difference between the
// given east and west longitudes
func eastDelta(e, w float64) float64 {
	e = normEasting(e)
	w = normEasting(w)
	if e < w {
		return 360 + e - w
	}
	return e - w
}

// normEasting returns the given longitude in dec degress normalized to be within [-180,180]
func normEasting(deg float64) float64 {
	// go's Mod fcn preserves sign on first param
	if deg < -180 {
		return math.Mod(deg+180, 360) + 180
	}
	if deg > 180 {
		return math.Mod(deg-180, 360) - 180
	}
	return deg
}

func resizeFixToJpg(outFile, inFile string, maxPixArea int) error {
	// param order super sensitive
	cmd := exec.Command("convert", "-resize", "@"+fmt.Sprintf("%v", maxPixArea), inFile, "-strip", "-interlace", "none", outFile)
	glog.Infof("About to run: %#v\n", cmd.Args)
	_, err := cmd.Output()
	if err != nil {
		return err
	}
	return nil
}

func fixToJpg(outFile, inFile string) error {
	cmd := exec.Command("convert", inFile, "-strip", "-interlace", "none", outFile)
	glog.Infof("About to run: %#v\n", cmd.Args)
	_, err := cmd.Output()
	if err != nil {
		return err
	}
	return nil
}

func chopToJpgs(fixedJpg, outDir, baseName string) error {
	outFile := filepath.Join(outDir, baseName+"_tile_%03d.jpg")
	cmd := exec.Command("convert", "-crop", "1024x1024", fixedJpg, "+adjoin", outFile)
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
