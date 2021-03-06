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

// cutkmz - Command line Go App that converts JPG geographic map
// images into overlay KMZs. The kmz subscommand produces a KMZ some
// Garmin GPS devices can handle.  The bigkmz subcommand produces
// higher resolution KMZs suitable for use with Google Earth etc.
//
// This software requires ImageMagick to be installed on your system.
//
// Get cutkmz (ensure you have Go installed already #golang):
//
//    go get github.com/msample/cutkmz
//
// Use it:
//
//    cutkmz kmz mymap_49.470608_49.336874_-122.980874_-123.131480.jpg
//
//    cutkmz kmz --help
//
package main

import "github.com/msample/cutkmz/cmd"

func main() {
	cmd.Execute()
}
