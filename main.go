package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"log"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/tedpearson/ambientweatherexporter/weather"
)

var (
	version   = "development"
	goVersion = "unknown"
	buildDate = "unknown"
)

func main() {
	port := flag.Int("port", 2184, "Http server port to listen on")
	prefix := flag.String("prefix", "",
		"add metrics prefix %s_(metric_name)")
	be_verbose := flag.Bool("verbose", false,
		"More verbose logging.")
	name := flag.String("station-name", "",
		"Weather station name for the 'name' label on the metrics")
	versionFlag := flag.Bool("v", false, "Show version and exit")
	flag.Parse()

	log.SetFlags(log.Flags() &^ (log.Ldate | log.Ltime))
	log.Println(fmt.Sprintf("ambientweatherexporter version %s built on %s with %s", version, buildDate, goVersion))

	if *versionFlag {
		os.Exit(0)
	}
	registry := prometheus.NewRegistry()
	factory := promauto.With(registry)
	http.Handle("/data/report/", weather.NewParser(*name, *prefix, *be_verbose, &factory))
	http.Handle("/metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
	err := http.ListenAndServe(fmt.Sprintf(":%d", *port), nil)
	if err != nil {
		panic(err)
	}
}
