package main

import (
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	// "os/exec"
	// "strings"
	"sync"

	"github.com/ryankurte/go-mapbox/lib"
	// "github.com/sjsafranek/goutils"
	"github.com/sjsafranek/goutils/shell"
)

// xyz
type xyz struct {
	x uint64
	y uint64
	z uint64
}

func NewTerrainMap(token string) (*TerrainMap, error) {
	mb, err := mapbox.NewMapbox(MAPBOX_TOKEN)
	return &TerrainMap{MapBox: mb, zoom: 2}, err
}

type TerrainMap struct {
	MapBox *mapbox.Mapbox
	zoom   int
}

func (self *TerrainMap) SetZoom(zoom int) {
	self.zoom = zoom
}

func (self *TerrainMap) Render(minLat, maxLat, minLng, maxLng float64, zoom int, outFile string) {
	tiles := GetTileNamesFromMapView(minLat, maxLat, minLng, maxLng, zoom)

	log.Printf(`Parameters:
	extent:	[%v, %v, %v, %v]
	zoom:	%v
	tiles:	%v`, minLat, maxLat, minLng, maxLng, zoom, len(tiles))

	if 100 < len(tiles) {
		panic(errors.New("Too many map tiles. Please raise map zoom or change bounds"))
	}

	// create temp directroy
	directory, err := ioutil.TempDir(os.TempDir(), "terrain-rgb")
	if nil != err {
		panic(err)
	}
	defer os.RemoveAll(directory)
	//.end

	var workwg sync.WaitGroup
	queue := make(chan xyz, numWorkers*2)

	log.Println("Spawning workers")
	for i := 0; i < numWorkers; i++ {
		go terrainWorker(self.MapBox, queue, directory, &workwg)
	}

	log.Println("Requesting tiles")
	for _, v := range tiles {
		workwg.Add(1)
		queue <- v
	}

	close(queue)

	workwg.Wait()

	log.Println("Building GeoTIFF")
	self.createGeoTIFFScript()

	// shell.RunScript("/bin/sh", "./build_tiff.sh", directory, outFile)
	shell.RunScript("/bin/sh", "./build_geotiff.sh", directory, outFile)

	// cleanup
	os.Remove("build_geotiff.sh")
}

func (self TerrainMap) createGeoTIFFScript() {
	script := `
#!/bin/bash

DIRECTORY=$1
OUT_FILE=$2

# build tiff from each file
echo "Building tif files from csv map tiles"
for FILE in $DIRECTORY/*.csv; do
    GEOTIFF="${FILE%.*}.tif"
    XYZ="${FILE%.*}.xyz"

    echo "Building $XYZ from $FILE"
    $(echo head -n 1 $FILE) >  "$XYZ"; \
        tail -n +2 $FILE | sort -n -t ',' -k2 -k1 >> "$XYZ";

    echo "Building $GEOTIFF from $XYZ"
    gdal_translate "$XYZ" "$GEOTIFF"
done

echo "Merging tif files to $OUT_FILE"
gdalwarp --config GDAL_CACHEMAX 3000 -wm 3000 $DIRECTORY/*.tif $OUT_FILE
	`
	file, err := os.Create("build_geotiff.sh")
	if nil != err {
		log.Fatal(err)
	}
	defer file.Close()
	fmt.Fprintf(file, script)
}
