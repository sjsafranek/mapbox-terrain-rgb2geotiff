package main

import (
	"fmt"
	"log"
	"math"
	"os"

	"github.com/lukeroth/gdal"
	"github.com/ryankurte/go-mapbox/lib/base"
	"github.com/ryankurte/go-mapbox/lib/maps"
)

// degTorad converts degree to radians.
func degTorad(deg float64) float64 {
	return deg * math.Pi / 180
}

// deg2num converts latlng to tile number
func deg2num(latDeg float64, lonDeg float64, zoom int) (int, int) {
	latRad := degTorad(latDeg)
	n := math.Pow(2.0, float64(zoom))
	xtile := int((lonDeg + 180.0) / 360.0 * n)
	ytile := int((1.0 - math.Log(math.Tan(latRad)+(1/math.Cos(latRad)))/math.Pi) / 2.0 * n)
	return xtile, ytile
}

type TerrainTile struct {
	maps *maps.Maps
	tile *maps.Tile
	x    uint64
	y    uint64
	z    uint64
}

func (self *TerrainTile) X() uint64 {
	return self.x
}

func (self *TerrainTile) Y() uint64 {
	return self.y
}

func (self *TerrainTile) Z() uint64 {
	return self.z
}

func (self *TerrainTile) Fetch() error {
	log.Println("Fetch tile", self.x, self.y, self.z)
	highDPI := false
	tile, err := self.maps.GetTile(maps.MapIDTerrainRGB, self.x, self.y, self.z, maps.MapFormatPngRaw, highDPI)
	if nil != err {
		return err
	}
	self.tile = tile
	return nil
}

func (self *TerrainTile) GetTile() *maps.Tile {
	return self.tile
}

func (self *TerrainTile) WriteXYZ(fh *os.File) error {

	if nil == self.tile {
		err := self.Fetch()
		if nil != err {
			return err
		}
	}

	fmt.Fprintf(fh, "x,y,z\n")

	self.forEach(func(longitude, latitude, elevation float64) {
		line := fmt.Sprintf("%v,%v,%v\n", longitude, latitude, elevation)
		fmt.Fprintf(fh, line)
	})

	return nil
}

func (self *TerrainTile) forEach(clbk func(float64, float64, float64)) error {
	// y axis needs to be sorted for xyz files
	for y := 0; y < self.tile.Bounds().Max.Y; y++ {
		for x := 0; x < self.tile.Bounds().Max.X; x++ {
			// get location for each pixel
			loc, err := self.tile.PixelToLocation(float64(x), float64(y))
			if nil != err {
				log.Fatal(err)
				continue
			}

			// get altitude
			ll := base.Location{Latitude: loc.Latitude, Longitude: loc.Longitude}
			elevation, err := self.tile.GetAltitude(ll)
			if nil != err {
				log.Println(err)
			}

			// run callback
			clbk(loc.Longitude, loc.Latitude, elevation)
		}
	}
	return nil
}

//
func (self *TerrainTile) ToArray() ([]float64, error) {

	if nil == self.tile {
		err := self.Fetch()
		if nil != err {
			return []float64{}, err
		}
	}

	var buffer [256 * 256]float64

	for y := 0; y < self.tile.Bounds().Max.Y; y++ {
		for x := 0; x < self.tile.Bounds().Max.X; x++ {

			loc, err := self.tile.PixelToLocation(float64(x), float64(y))
			if nil != err {
				log.Fatal(err)
			}

			ll := base.Location{Latitude: loc.Latitude, Longitude: loc.Longitude}

			elevation, err := self.tile.GetAltitude(ll)
			if nil != err {
				log.Println(err)
			}

			pos := x + y*256
			buffer[pos] = elevation
		}
	}

	// https://github.com/lukeroth/gdal/issues/43
	var values []float64
	for _, v := range buffer {
		values = append(values, v)
	}

	return values, nil
}

// (xmin, ymin and xmax, ymax)
func (self *TerrainTile) Extent() ([4]float64, error) {
	bounds := self.tile.Bounds()

	max_x := float64(bounds.Max.X)
	max_y := float64(bounds.Max.Y)
	top, err := self.tile.PixelToLocation(max_x, max_y)
	if nil != err {
		return [4]float64{}, err
	}

	min_x := float64(bounds.Min.X)
	min_y := float64(bounds.Min.Y)
	bottom, err := self.tile.PixelToLocation(min_x, min_y)
	if nil != err {
		return [4]float64{}, err
	}

	return [4]float64{bottom.Longitude, bottom.Latitude, top.Longitude, top.Latitude}, nil
}

// BUG
// 	- The data shifts to the south east...
func (self *TerrainTile) WriteGeoTiff(filename string) error {
	// extract image data to array
	buffer, err := self.ToArray()
	if err != nil {
		return err
	}

	extent, err := self.Extent()
	if nil != err {
		return err
	}

	log.Println("Loading driver")
	driver, err := gdal.GetDriverByName("GTiff")
	if err != nil {
		return err
	}
	log.Println("Creating dataset")
	dataset := driver.Create(filename, 256, 256, 1, gdal.Byte, nil)
	defer dataset.Close()

	log.Println("Setting projection")
	spatialRef := gdal.CreateSpatialReference("")
	spatialRef.FromEPSG(4326)
	srString, err := spatialRef.ToWKT()
	if err != nil {
		return err
	}
	dataset.SetProjection(srString)

	log.Println("Setting geotransform")
	we_resolution := (extent[2] - extent[0])/256
	ns_resolution := (extent[1] - extent[3])/256
	// https://gis.stackexchange.com/questions/165950/gdal-setgeotransform-does-not-work-as-expected
	// geotransform = ([ your_top_left_x, 30, 0, your_top_left_y, 0, -30 ])
	dataset.SetGeoTransform([6]float64{extent[2], we_resolution, 0, extent[3], 0, -1*(ns_resolution)})

	log.Println("Writing to raster band")
	raster := dataset.RasterBand(1)
	err = raster.IO(gdal.Write, 0, 0, 256, 256, buffer, 256, 256, 0, 0)
	if err != nil {
		return err
	}

	return nil
}
