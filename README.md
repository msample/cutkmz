# cutkmz
--
cutkmz - Command line Go App that converts JPG geographic map images into
overlay KMZs. The kmz subscommand produces a KMZ some Garmin GPS devices can
handle. The bigkmz subcommand produces higher resolution KMZs suitable for use
with Google Earth etc.

This software requires ImageMagick to be installed on your system.

Get cutkmz (ensure you have Go installed already #golang):

    go get github.com/msample/cutkmz

Use it:

    cutkmz kmz mymap_49.470608_49.336874_-122.980874_-123.131480.jpg

    cutkmz kmz --help
