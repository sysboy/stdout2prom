// Takes your stdout and puts it into Prometheus!
package main

import (
	"bufio"
	"flag"
	"fmt"
	"github.com/prometheus/client_golang/prometheus"
	"gopkg.in/yaml.v2"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"regexp"
	"runtime/pprof"
	"strconv"
)

//
// Data structure to hold all of our interesting metrics, this
// is part of this is filled from the config yaml file, then metrics
// and regexes are created for each metric.
//
type Data struct {
	Basename   string `yaml:"basename,omitempty"`
	EatMatches bool   `yaml:"eatMatches"`
	EatAll     bool   `yaml:"eatAll"`
	Listen     string `yaml:"listen"`
	Path       string `yaml:"path"`
	Metrics    []struct {
		Name        string `yaml:"name,omitempty"`
		Description string `yaml:"description,omitempty"`
		Regex       string `yaml:"regex,omitempty"`
		Gauge       bool   `yaml:"gauge"`
		Collector   prometheus.Collector
		Compiled    *regexp.Regexp
	} `yaml:"metrics,omitempty"`
}

var (
	// some defaults
	cnf = Data{
		Listen:     ":9000",
		Path:       "/metrics",
		EatMatches: false,
		EatAll:     false,
	}

	// parameters
	debug      = flag.Bool("debug", false, "Display more of the inner workings.")
	config     = flag.String("config", "metrics.yml", "Config file.")
	cpuprofile = flag.String("cpuprofile", "", "write cpu profile to file")

	// some metrics for ourself
	totalLines = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "stdout2prom_lines_parsed_total",
			Help: "Total lines read from stdin",
		},
	)

	bytesRead = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "stdout2prom_bytes_read_total",
			Help: "Total number of bytes read from stdin",
		},
	)

	matchedLines = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "stdout2prom_matched_lines_total",
			Help: "Total lines that matched one of the regexes",
		},
	)

	badFloats = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "stdout2prom_bad_floats_total",
			Help: "Total lines that failed to convert correctly",
		},
	)
)

func main() {

	flag.Parse()
	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			log.Fatal(err)
		}
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}
	data, err := ioutil.ReadFile(*config)
	if err != nil {
		fmt.Println("Failed to open config file")
		panic(err.Error())
	}

	err = yaml.Unmarshal(data, &cnf)
	if err != nil {
		fmt.Println("Failed to parse YAML file")
		panic(err.Error())
	}

	for index, metric := range cnf.Metrics {

		metricName := cnf.Basename + "_" + metric.Name

		if metric.Gauge {
			cnf.Metrics[index].Collector = prometheus.NewGauge(
				prometheus.GaugeOpts{
					Name: metricName,
					Help: metric.Description,
				})

		} else {

			cnf.Metrics[index].Collector = prometheus.NewCounter(
				prometheus.CounterOpts{
					Name: metricName,
					Help: metric.Description,
				})
		}

		prometheus.MustRegister(cnf.Metrics[index].Collector)
		cnf.Metrics[index].Compiled = regexp.MustCompile(metric.Regex)

		if *debug {
			fmt.Printf("Added metric for %s\n", metricName)
		}

	}

	//
	// these our our own metrics to track what we processed
	//
	prometheus.MustRegister(totalLines)
	prometheus.MustRegister(bytesRead)
	prometheus.MustRegister(matchedLines)

	http.Handle(cnf.Path, prometheus.Handler())
	go http.ListenAndServe(cnf.Listen, nil)

	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := scanner.Text()

		totalLines.Inc()
		bytesRead.Add(float64(len(line)))

		matchFound := false
		for _, metric := range cnf.Metrics {
			if *debug {
				fmt.Printf("Testing against %s\n", metric.Name)
			}

			result := metric.Compiled.FindStringSubmatch(line)

			if result != nil {

				matchedLines.Inc()
				matchFound = true

				if metric.Gauge {
					if s, err := strconv.ParseFloat(result[1], 64); err == nil {
						metric.Collector.(prometheus.Gauge).Set(s)
					} else {
						badFloats.Inc()
					}

				} else {
					metric.Collector.(prometheus.Counter).Inc()
				}

				if *debug {
					fmt.Printf(" ** Match **\n")
				}
			}
		}

		if cnf.EatAll {
			continue
		}
		if matchFound && cnf.EatMatches {
			continue
		}
		fmt.Println(line)

	}

}
