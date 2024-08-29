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
	name                  string
	be_verbose            bool
	metric_prefix         string
	temperature           *prometheus.GaugeVec
	battery               *prometheus.GaugeVec // 1 = ok; 0 = low
	humidity              *prometheus.GaugeVec
	barometer             *prometheus.GaugeVec
	windDir               *prometheus.GaugeVec
	windSpeedMph          *prometheus.GaugeVec
	solarRadiation        *prometheus.GaugeVec
	rainIn                *prometheus.GaugeVec
	ultraviolet           *prometheus.GaugeVec
	lightning_strikes     *prometheus.GaugeVec
	lightning_last_strike *prometheus.GaugeVec
	lightning_distance    *prometheus.GaugeVec
	stationtype           *prometheus.GaugeVec
}

func NewParser(name string, metric_prefix string, be_verbose bool, factory *promauto.Factory) *Parser {
	return &Parser{
		name:                  name,
		be_verbose:            be_verbose,
		metric_prefix:         metric_prefix,
		temperature:           newGauge(factory, metric_prefix, "temperature", "temperature Temperature in fahrenheit", "remote_adress", "name", "sensor"),
		battery:               newGauge(factory, metric_prefix, "battery", "battery", "remote_adress", "name", "sensor"),
		humidity:              newGauge(factory, metric_prefix, "humidity", "humidity", "remote_adress", "name", "sensor"),
		barometer:             newGauge(factory, metric_prefix, "barometer", "barometer", "remote_adress", "name", "type"),
		windDir:               newGauge(factory, metric_prefix, "wind_dir", "wind_dir", "remote_adress", "name", "period"),
		windSpeedMph:          newGauge(factory, metric_prefix, "wind_speed_mph", "wind_speed_mph", "remote_adress", "name", "type"),
		solarRadiation:        newGauge(factory, metric_prefix, "solar_radiation", "Solar radiation in W/m2", "remote_adress", "name"),
		rainIn:                newGauge(factory, metric_prefix, "rain_in", "Rain in inches", "remote_adress", "name", "period"),
		ultraviolet:           newGauge(factory, metric_prefix, "ultraviolet", "Ultra Violet index 1-10", "remote_adress", "name"),
		lightning_strikes:     newGauge(factory, metric_prefix, "lightning_strikes", "lightning_strikes", "remote_adress", "name", "period"),
		lightning_last_strike: newGauge(factory, metric_prefix, "lightning_last_strike", "in seconds since Epoch", "remote_adress", "name"),
		lightning_distance:    newGauge(factory, metric_prefix, "lightning_distance", "last lightning strike distance in km", "remote_adress", "name"),
		stationtype:           newGauge(factory, metric_prefix, "stationtype_info", "stationtype_info", "remote_adress", "name", "type"),
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
	var re = regexp.MustCompile(`^(.*):\d+$`)
	remote_adress := re.ReplaceAllString(req.RemoteAddr, "$1")

	// remove PASSKEY value from url
	re = regexp.MustCompile(`&PASSKEY=[^&]*`)
	req.URL.Path = re.ReplaceAllString(req.URL.Path, "&PASSKEY=******")

	p.Log("sample submitted by remote_adress %s: %s", remote_adress, req.URL.Path)

	// make url more easilily parseable
	queryStr := strings.Replace(req.URL.Path, "/data/report/", "", 1)
	// respond immediately
	resp.WriteHeader(http.StatusNoContent)
	values, err := url.ParseQuery(queryStr)
	if err != nil {
		log.Printf("Failed to parse weather observation from request url: %+v", err)
	}
	p.Parse(remote_adress, values)
}

func (p *Parser) Log(format string, a ...any) {
	if p.be_verbose {
		log.Printf(format, a...)
	}
}

func (p *Parser) Parse(remote_adress string, values url.Values) {
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
			updateGauge(p.temperature.WithLabelValues(remote_adress,p.name, iStr))(parseValue(fmt.Sprintf("temp%df", i)))
			updateGauge(p.battery.WithLabelValues(remote_adress,p.name, iStr))(parseValue("batt" + iStr))
		} else {
			p.battery.DeleteLabelValues(p.name, iStr)
			p.temperature.DeleteLabelValues(p.name, iStr)
		}
		if values.Has("soilhum" + iStr) {
			updateGauge(p.humidity.WithLabelValues(remote_adress,p.name, "soil"+iStr))(parseValue("soilhum" + iStr))
			updateGauge(p.battery.WithLabelValues(remote_adress,p.name, "soil"+iStr))(parseValue("battsm" + iStr))
		} else {
			p.humidity.DeleteLabelValues(p.name, "soil"+iStr)
			p.battery.DeleteLabelValues(p.name, "soil"+iStr)
		}
		if values.Has("humidity" + iStr) {
			updateGauge(p.humidity.WithLabelValues(remote_adress,p.name, iStr))(parseValue("humidity" + iStr))
		} else {
			p.humidity.DeleteLabelValues(p.name, iStr)
		}
	}

	updateGauge(p.temperature.WithLabelValues(remote_adress,p.name, "indoor"))(parseValue("tempinf"))
	tempF, err := parseValue("tempf")
	if err == nil {
		p.temperature.WithLabelValues(remote_adress,p.name, "outdoor").Set(tempF)
		feelsLike := tempF
		windSpeedMph, err := parseValue("windspeedmph")
		if err == nil {
			p.windSpeedMph.WithLabelValues(remote_adress,p.name, "sustained").Set(windSpeedMph)
			if tempF <= 40 {
				feelsLike = calculateWindChill(tempF, windSpeedMph)
			}
		}
		humidity, err := parseValue("humidity")
		if err == nil {
			p.humidity.WithLabelValues(remote_adress,p.name, "outdoor").Set(humidity)
			p.temperature.WithLabelValues(remote_adress,p.name, "dewpoint").Set(calculateDewPoint(tempF, humidity))
			if tempF >= 80 {
				feelsLike = calculateHeatIndex(tempF, humidity)
			}
		}
		p.temperature.WithLabelValues(remote_adress,p.name, "feelsLike").Set(feelsLike)
	}

	updateGauge(p.battery.WithLabelValues(remote_adress,p.name, "outdoor"))(parseValue("battout"))
	updateGauge(p.battery.WithLabelValues(remote_adress,p.name, "indoor"))(parseValue("battin"))
	updateGauge(p.battery.WithLabelValues(remote_adress,p.name, "lightning"))(parseValue("batt_lightning"))
	updateGauge(p.humidity.WithLabelValues(remote_adress,p.name, "indoor"))(parseValue("humidityin"))
	updateGauge(p.barometer.WithLabelValues(remote_adress,p.name, "relative"))(parseValue("baromrelin"))
	updateGauge(p.barometer.WithLabelValues(remote_adress,p.name, "absolute"))(parseValue("baromabsin"))
	updateGauge(p.windDir.WithLabelValues(remote_adress,p.name, "current"))(parseValue("winddir"))
	updateGauge(p.windDir.WithLabelValues(remote_adress,p.name, "avg10m"))(parseValue("winddir_avg10m"))
	updateGauge(p.windSpeedMph.WithLabelValues(remote_adress,p.name, "gusts"))(parseValue("windgustmph"))
	updateGauge(p.solarRadiation.WithLabelValues(remote_adress,p.name))(parseValue("solarradiation"))
	updateGauge(p.rainIn.WithLabelValues(remote_adress,p.name, "hourly"))(parseValue("hourlyrainin"))
	updateGauge(p.rainIn.WithLabelValues(remote_adress,p.name, "daily"))(parseValue("dailyrainin"))
	updateGauge(p.rainIn.WithLabelValues(remote_adress,p.name, "weekly"))(parseValue("weeklyrainin"))
	updateGauge(p.rainIn.WithLabelValues(remote_adress,p.name, "monthly"))(parseValue("monthlyrainin"))
	updateGauge(p.rainIn.WithLabelValues(remote_adress,p.name, "yearly"))(parseValue("yearlyrainin"))
	updateGauge(p.rainIn.WithLabelValues(remote_adress,p.name, "total"))(parseValue("totalrainin"))
	updateGauge(p.rainIn.WithLabelValues(remote_adress,p.name, "event"))(parseValue("eventrainin"))
	updateGauge(p.ultraviolet.WithLabelValues(remote_adress,p.name))(parseValue("uv"))
	updateGauge(p.lightning_strikes.WithLabelValues(remote_adress,p.name, "day"))(parseValue("lightning_day"))
	updateGauge(p.lightning_distance.WithLabelValues(remote_adress,p.name))(parseValue("lightning_distance"))
	updateGauge(p.lightning_last_strike.WithLabelValues(remote_adress,p.name))(parseValue("lightning_time"))

	stationType, station_err := parseString("stationtype")
	if err == station_err {
		updateGauge(p.stationtype.WithLabelValues(remote_adress,p.name, stationType))(float64(1), nil)
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
