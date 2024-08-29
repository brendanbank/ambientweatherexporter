package weather

import (
	"fmt"
	"log"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type Parser struct {
	name           string
	be_verbose     bool
	metric_prefix  string
	temperature    *prometheus.GaugeVec
	battery        *prometheus.GaugeVec // 1 = ok; 0 = low
	humidity       *prometheus.GaugeVec
	barometer      *prometheus.GaugeVec
	windDir        *prometheus.GaugeVec
	windSpeedMph   *prometheus.GaugeVec
	solarRadiation *prometheus.GaugeVec
	rainIn         *prometheus.GaugeVec
	ultraviolet    *prometheus.GaugeVec
	lightning_strikes      *prometheus.GaugeVec
	lightning_last_strike      *prometheus.GaugeVec
	lightning_distance      *prometheus.GaugeVec
	stationtype    *prometheus.GaugeVec
}

func NewParser(name string, prefix string, be_verbose bool, factory *promauto.Factory) *Parser {
	metric_prefix := ""
	if prefix != "" {
		metric_prefix = prefix
	}

	return &Parser{
		name:           name,
		be_verbose:     be_verbose,
		metric_prefix:  metric_prefix,
		temperature:    newGauge(factory, metric_prefix, "temperature", "temperature Temperature in fahrenheit", "name", "sensor"),
		battery:        newGauge(factory, metric_prefix, "battery", "battery", "name", "sensor"),
		humidity:       newGauge(factory, metric_prefix, "humidity", "humidity", "name", "sensor"),
		barometer:      newGauge(factory, metric_prefix, "barometer", "barometer", "name", "type"),
		windDir:        newGauge(factory, metric_prefix, "wind_dir", "barometer", "name", "period"),
		windSpeedMph:   newGauge(factory, metric_prefix, "wind_speed_mph", "wind_speed_mph", "name", "type"),
		solarRadiation: newGauge(factory, metric_prefix, "solar_radiation", "Solar radiation in W/m2", "name"),
		rainIn:         newGauge(factory, metric_prefix, "rain_in", "Rain in inches", "name", "period"),
		ultraviolet:    newGauge(factory, metric_prefix, "ultraviolet", "index 1-10", "name"),
		lightning_strikes:      newGauge(factory, metric_prefix, "lightning_strikes", "lightning_strikes", "name", "period"),
		lightning_last_strike:      newGauge(factory, metric_prefix, "lightning_last_strike", "in seconds since Epoch", "name"),
		lightning_distance:      newGauge(factory, metric_prefix, "lightning_distance", "last lightning strike distance in km", "name"),
		stationtype:    newGauge(factory, metric_prefix, "stationtype_info", "stationtype_info", "name", "type"),
	}
}

func newGauge(factory *promauto.Factory, metric_prefix string, name string, help string, labels ...string) *prometheus.GaugeVec {
	opts := prometheus.GaugeOpts{
		Name:      name,
		Help:      help,
		Namespace: metric_prefix,
	}
	return factory.NewGaugeVec(opts, labels)
}

func (p *Parser) ServeHTTP(resp http.ResponseWriter, req *http.Request) {
	// parse request url.
	if p.be_verbose {
		var re = regexp.MustCompile(`&PASSKEY=[^&]*`)
		s := re.ReplaceAllString(req.URL.EscapedPath(), "&PASSKEY=******")
		fmt.Printf("sample submitted from %s: %s\n", req.RemoteAddr, s)
	}
	// make url more easilily parseable
	queryStr := strings.Replace(req.URL.Path, "/data/report/", "", 1)
	// respond immediately
	resp.WriteHeader(http.StatusNoContent)
	values, err := url.ParseQuery(queryStr)
	if err != nil {
		log.Printf("Failed to parse weather observation from request url: %+v", err)
	}
	p.Parse(values)
}

func (p *Parser) Parse(values url.Values) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Failed to parse incoming request: %+v", r)
		}
	}()

	parseString := func(name string) (string, error) {
		array, ok := values[name]
		if !ok {
			return "", fmt.Errorf("no such param: %s", name)
		}
		str := strings.ReplaceAll(array[0], "\n", "")
		str = strings.ReplaceAll(str, "\r", "")

		return str, nil
	}

	parseValue := func(name string) (float64, error) {
		array, ok := values[name]
		if !ok {
			return 0, fmt.Errorf("no such param: %s", name)
		}
		first := strings.ReplaceAll(array[0], "\n", "")
		first = strings.ReplaceAll(first, "\r", "")
		value, err := strconv.ParseFloat(first, 64)
		if err != nil {
			e := fmt.Errorf("failed to parse value: '%s': %+v", first, err)
			log.Println(e)
			return 0, e
		}
		return value, nil
	}

	for i := 1; i <= 10; i++ {
		iStr := strconv.Itoa(i)
		if values.Has(fmt.Sprintf("temp%df", i)) {
			updateGauge(p.temperature.WithLabelValues(p.name, iStr))(parseValue(fmt.Sprintf("temp%df", i)))
			updateGauge(p.battery.WithLabelValues(p.name, iStr))(parseValue("batt" + iStr))
		} else {
			p.battery.DeleteLabelValues(p.name, iStr)
			p.temperature.DeleteLabelValues(p.name, iStr)
		}
		if values.Has("soilhum" + iStr) {
			updateGauge(p.humidity.WithLabelValues(p.name, "soil"+iStr))(parseValue("soilhum" + iStr))
			updateGauge(p.battery.WithLabelValues(p.name, "soil"+iStr))(parseValue("battsm" + iStr))
		} else {
			p.humidity.DeleteLabelValues(p.name, "soil"+iStr)
			p.battery.DeleteLabelValues(p.name, "soil"+iStr)
		}
		if values.Has("humidity" + iStr) {
			updateGauge(p.humidity.WithLabelValues(p.name, iStr))(parseValue("humidity" + iStr))
		} else {
			p.humidity.DeleteLabelValues(p.name, iStr)
		}
	}

	updateGauge(p.temperature.WithLabelValues(p.name, "indoor"))(parseValue("tempinf"))
	tempF, err := parseValue("tempf")
	if err == nil {
		p.temperature.WithLabelValues(p.name, "outdoor").Set(tempF)
		feelsLike := tempF
		windSpeedMph, err := parseValue("windspeedmph")
		if err == nil {
			p.windSpeedMph.WithLabelValues(p.name, "sustained").Set(windSpeedMph)
			if tempF <= 40 {
				feelsLike = calculateWindChill(tempF, windSpeedMph)
			}
		}
		humidity, err := parseValue("humidity")
		if err == nil {
			p.humidity.WithLabelValues(p.name, "outdoor").Set(humidity)
			p.temperature.WithLabelValues(p.name, "dewpoint").Set(calculateDewPoint(tempF, humidity))
			if tempF >= 80 {
				feelsLike = calculateHeatIndex(tempF, humidity)
			}
		}
		p.temperature.WithLabelValues(p.name, "feelsLike").Set(feelsLike)
	}

	updateGauge(p.battery.WithLabelValues(p.name, "outdoor"))(parseValue("battout"))
	updateGauge(p.battery.WithLabelValues(p.name, "indoor"))(parseValue("battin"))
	updateGauge(p.battery.WithLabelValues(p.name, "lightning"))(parseValue("batt_lightning"))
	updateGauge(p.humidity.WithLabelValues(p.name, "indoor"))(parseValue("humidityin"))
	updateGauge(p.barometer.WithLabelValues(p.name, "relative"))(parseValue("baromrelin"))
	updateGauge(p.barometer.WithLabelValues(p.name, "absolute"))(parseValue("baromabsin"))
	updateGauge(p.windDir.WithLabelValues(p.name, "current"))(parseValue("winddir"))
	updateGauge(p.windDir.WithLabelValues(p.name, "avg10m"))(parseValue("winddir_avg10m"))
	updateGauge(p.windSpeedMph.WithLabelValues(p.name, "gusts"))(parseValue("windgustmph"))
	updateGauge(p.solarRadiation.WithLabelValues(p.name))(parseValue("solarradiation"))
	updateGauge(p.rainIn.WithLabelValues(p.name, "hourly"))(parseValue("hourlyrainin"))
	updateGauge(p.rainIn.WithLabelValues(p.name, "daily"))(parseValue("dailyrainin"))
	updateGauge(p.rainIn.WithLabelValues(p.name, "weekly"))(parseValue("weeklyrainin"))
	updateGauge(p.rainIn.WithLabelValues(p.name, "monthly"))(parseValue("monthlyrainin"))
	updateGauge(p.rainIn.WithLabelValues(p.name, "yearly"))(parseValue("yearlyrainin"))
	updateGauge(p.rainIn.WithLabelValues(p.name, "total"))(parseValue("totalrainin"))
	updateGauge(p.rainIn.WithLabelValues(p.name, "event"))(parseValue("eventrainin"))
	updateGauge(p.ultraviolet.WithLabelValues(p.name))(parseValue("uv"))
	updateGauge(p.lightning_strikes.WithLabelValues(p.name, "day"))(parseValue("lightning_day"))
	updateGauge(p.lightning_distance.WithLabelValues(p.name))(parseValue("lightning_distance"))
	updateGauge(p.lightning_last_strike.WithLabelValues(p.name))(parseValue("lightning_time"))

	stationType, station_err := parseString("stationtype")
	if err == station_err {
		updateGauge(p.stationtype.WithLabelValues(p.name, stationType))(float64(1), nil)
	}
}

func updateGauge(gauge prometheus.Gauge) func(float64, error) {
	return func(value float64, err error) {
		if err == nil {
			gauge.Set(value)
		}
	}
}

func calculateWindChill(tempF float64, windSpeedMph float64) float64 {
	if tempF > 40 || windSpeedMph < 5 {
		return tempF
	}
	windExp := math.Pow(windSpeedMph, 0.16)
	return 35.74 + (0.6215 * tempF) - (35.75 * windExp) + (0.4275 * tempF * windExp)
}

// following equation from https://www.wpc.ncep.noaa.gov/html/heatindex_equation.shtml
func calculateHeatIndex(tempF float64, rh float64) float64 {
	if tempF < 80 {
		return tempF
	}
	simpleHI := 0.5 * (tempF + 61 + ((tempF - 68) * 1.2) + (rh * .094))
	if simpleHI < 80 {
		return simpleHI
	}
	hi := -42.379 +
		2.04901523*tempF +
		10.14333127*rh -
		.22475541*tempF*rh -
		.00683783*tempF*tempF -
		.05481717*rh*rh +
		.00122874*tempF*tempF*rh +
		.00085282*tempF*rh*rh -
		.00000199*tempF*tempF*rh*rh
	if rh < 13 && tempF >= 80 && tempF <= 112 {
		hi = hi - ((13-rh)/4)*math.Sqrt((17-math.Abs(tempF-95))/17)
	} else if rh > 85 && tempF >= 80 && tempF <= 87 {
		hi = hi + ((rh-85)/10)*((87-tempF)/5)
	}
	return hi
}

func calculateDewPoint(tempF float64, rh float64) float64 {
	a := 17.625
	b := 243.04
	t := (tempF - 32) * 5 / 9
	alpha := math.Log(rh/100) + ((a * t) / (b + t))
	return (b * alpha / (a - alpha) * 9 / 5) + 32
}
