package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"net/http"

	gtfs "github.com/DanielOaks/go.gtfs"
	"github.com/DanielOaks/trip-status/transit_realtime"
	"github.com/docopt/docopt-go"
	"github.com/golang/protobuf/proto"
	"github.com/mholt/archiver"
)

const (
	// SlowMinutesLate is the maximum number of minutes late a service can be before the route is marked as being 'slow'.
	// We can't be too strict here... it is TransLink, after all ;)
	SlowMinutesLate = 6
)

func main() {
	version := "0.1.0"
	usage := `generate-site.
Usage:
	generate-site run [--gtfs=<filename>] [--gtfsrt=<filename>] [--output=<directory>]
	generate-site -h | --help
	generate-site --version
Options:
	--gtfs=<filename>      GTFS static data zip [default: data/gtfs.zip].
	--gtfs-rt=<filename>   GTFS realtime data file (periodically curl'd from the RT feed location) [default: data/gtfs-rt].
	--output=<directory>   Directory to throw the site into [default: data/output/].

	-h --help    Show this screen.
	--version    Show version.`

	arguments, _ := docopt.Parse(usage, nil, true, version, false)

	if arguments["run"].(bool) {
		// extract static GTFS data to zip
		gtfsStaticDir, err := ioutil.TempDir("", "danieloaks-gtfs-status")
		if err != nil {
			log.Fatal(fmt.Sprintf("Could not create temp gtfs static dir: %s", err.Error()))
		}
		err = archiver.Unzip(arguments["--gtfs"].(string), gtfsStaticDir)
		if err != nil {
			log.Fatal(fmt.Sprintf("Could not extract GTFS data: %s", err.Error()))
		}

		fmt.Println("GTFS data extracted to", gtfsStaticDir)

		// parse static GTFS data
		feed := gtfs.Load(gtfsStaticDir, false)

		// load GTFS-RT file
		//TODO(dan): Use GTFS-RT file instead of grabbing it each run.
		resp, err := http.Get("https://gtfsrt.api.translink.com.au/Feed/SEQ")
		if err != nil {
			log.Fatal(fmt.Sprintf("Could not access feed: %s", err.Error()))
		}
		defer resp.Body.Close()
		body, err := ioutil.ReadAll(resp.Body)

		// read GTFS-RT feed
		transit := &transit_realtime.FeedMessage{}
		err = proto.Unmarshal(body, transit)
		if err != nil {
			log.Fatal(fmt.Sprintf("Could not unmarshal feed: %s", err.Error()))
		}
		for _, entity := range transit.Entity {
			if entity.Alert != nil {
				fmt.Println("Alert:", entity.Alert)
			}
			if entity.TripUpdate != nil && entity.TripUpdate.StopTimeUpdate != nil {
				// discover what type of vehicle this trip is for
				//var routeID string
				tripName := "This service"
				if entity.TripUpdate.Trip != nil {
					tripInfo := feed.Trips[*entity.TripUpdate.Trip.TripId]
					if tripInfo == nil {
						// skip trips not linked to a specific route
						// typically unplanned trips
						continue
					}
					//TODO(dan): For now we only process train lines
					if tripInfo.Route.VehicleType != gtfs.Rail {
						continue
					}
					tripName = tripInfo.Route.LongName
					//fmt.Println("Found rail trip", *entity.TripUpdate.Trip.TripId, "==", tripInfo.Route.ID, "==", tripInfo.Route.LongName)
				}

				for _, stoptime := range entity.TripUpdate.StopTimeUpdate {
					var arrivalDelay int32
					if stoptime.Arrival != nil && stoptime.Arrival.Delay != nil {
						arrivalDelay = *stoptime.Arrival.Delay
					}
					var departureDelay int32
					if stoptime.Departure != nil && stoptime.Departure.Delay != nil {
						departureDelay = *stoptime.Departure.Delay
					}
					if arrivalDelay > (60*SlowMinutesLate) || departureDelay > (60*SlowMinutesLate) {
						if arrivalDelay > departureDelay {
							fmt.Println(tripName, "is slow, should arrive", arrivalDelay, "seconds late")
						} else {
							fmt.Println(tripName, "is slow, should depart", departureDelay, "seconds late")
						}
					}
				}
			}
		}
	}
}
