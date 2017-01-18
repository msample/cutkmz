// Copyright © 2017 Mike Sample <mike@mikesample.com>
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.

package cmd

import (
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// bigkmzCmd represents the bigkmz command
var bigkmzCmd = &cobra.Command{
	Use:   "bigkmz",
	Short: "Create a KMZ with single jpg tile of the provided jpg. Suitable for Google Earth etc, not Garmins",
	Long: `Given a name-geo-anchored JPG this creates a KMZ file containing that single JPG. 
Use this for overlay maps you want to view in high resolution on Google Earth etc.

Unlike the kmz subcommand, the image is not sliced into titles to meet
device limitations. It is intended for higher resolution producing
KMZs for use on Google Earth and other apps that can handle large
images.

Input is the same name-geo-anchored JPG file as can be used with the
kmz subcommand.  For example using the same JPG file you can create a
KMZ for your Garmin with kmz subcom, and another KMZ with the bigkmz
subcommand for your PC.  E.g. in the Search and Rescue context, team
members can have the map on their GPSs in the field and a SAR manager
can use the bigkmz on Google Earth at the command post.

`,
	Run: func(cmd *cobra.Command, args []string) {
		if err := processBig(viper.GetViper(), args); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			fmt.Fprintf(os.Stderr, "see 'cutkmz bigkmz -h' for help\n")
			os.Exit(1)
		}
	},
}

func init() {
	RootCmd.AddCommand(bigkmzCmd)

	bigkmzCmd.Flags().IntP("max_pixels", "m", 0, "max pixel area, w x h (aka 'mega-pixels'). 0 means no limit, use image as is.")
	viper.BindPFlag("max_pixels", bigkmzCmd.Flags().Lookup("max_pixels"))

	bigkmzCmd.Flags().IntP("drawing_order", "d", 51, "Garmins make values > 50 visible. Tune if have overlapping overlays.")
	viper.BindPFlag("drawing_order", bigkmzCmd.Flags().Lookup("drawing_order"))

	bigkmzCmd.Flags().BoolP("keep_tmp", "k", false, "Don't delete intermediate files from $TMPDIR.")
	viper.BindPFlag("keep_tmp", bigkmzCmd.Flags().Lookup("keep_tmp"))

	bigkmzCmd.Flags().AddGoFlagSet(flag.CommandLine)
	flag.CommandLine.VisitAll(func(f *flag.Flag) {
		viper.BindPFlag(f.Name, bigkmzCmd.Flags().Lookup(f.Name))
	})
	flag.CommandLine.Parse(nil) // shut up 'not parsed' complaints
}

// processBig processes the name-geo-anchored files args into KMZs
// with a single (large) JPG.
//
// The max_pixels (width x height) can be used to reduce quality to
// desired pixel araea.  0, the default, means unlimited/leave the
// image as is.
func processBig(v *viper.Viper, args []string) error {
	maxPixels := v.GetInt("max_pixels")
	keepTmp := v.GetBool("keep_tmp")
	drawingOrder := v.GetInt("drawing_order")

	fmt.Printf("keep_tmp: %v, maxPixels: %v, drawing_order %v\n", keepTmp, maxPixels, drawingOrder)

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
		origMap, err := newMapTileFromFile(absImage, box[north], box[south], box[east], box[west])
		if err != nil {
			return fmt.Errorf("Error extracting image dimensions: %v", err)
		}
		tmpDir, err := ioutil.TempDir("", "cutkmz-")
		if err != nil {
			return fmt.Errorf("Error creating a temporary directory: %v", err)
		}
		tilesDir := filepath.Join(tmpDir, base, "tiles")
		err = os.MkdirAll(tilesDir, 0755)
		if err != nil {
			return fmt.Errorf("Error making tiles dir in tmp dir: %v", err)
		}

		fixedJpg := filepath.Join(tilesDir, base+"_tile_000.jpg") // one tile
		if maxPixels > 0 && maxPixels < (origMap.height*origMap.width) {
			resizeFixToJpg(fixedJpg, absImage, maxPixels)
		} else {
			// just copy the file, no de-interlace or stripping
			var in, out *os.File
			if out, err = os.Create(fixedJpg); err != nil {
				return err
			}
			if in, err = os.Open(absImage); err != nil {
				return err
			}
			if _, err = io.Copy(out, in); err != nil {
				return err
			}
		}

		fixedMap, err := newMapTileFromFile(fixedJpg, box[north], box[south], box[east], box[west])
		if err != nil {
			return err
		}

		var kdocWtr *os.File

		if kdocWtr, err = os.Create(filepath.Join(tmpDir, base, "doc.kml")); err != nil {
			return err
		}
		if err = startKML(kdocWtr, base); err != nil {
			return err
		}

		var relTPath string // file ref inside KML must be relative to kmz root
		if relTPath, err = filepath.Rel(filepath.Join(tmpDir, base), fixedMap.fpath); err != nil {
			return err
		}
		if err = kmlAddOverlay(kdocWtr, base, fixedMap.box, drawingOrder, relTPath); err != nil {
			return err
		}
		endKML(kdocWtr)
		kdocWtr.Close()
		var zf *os.File
		if zf, err = os.Create(base + "-big.kmz"); err != nil {
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
