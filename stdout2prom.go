// Takes your stdout and puts it into Prometheus!
package main

import (
	"bufio"
	"flag"
	"fmt"
	"github.com/prometheus/client_golang/prometheus"
	"gopkg.in/yaml.v2"
	"io/ioutil"
	"net/http"
	"os"
	"regexp"
	"strconv"
)

//
// Data structure to hold all of our interesting metrics, this
// is part of this is filled from the config yaml file, then metrics
// and regexes are created for each metric.
//
type Data struct {
	Basename string `yaml:"basename,omitempty"`
	Metrics  []struct {
		Name        string `yaml:"name,omitempty"`
		Description string `yaml:"description,omitempty"`
		Regex       string `yaml:"regex,omitempty"`
		PromType    string `yaml:"type,omitempty"`
		Collector   prometheus.Collector
		Compiled    *regexp.Regexp
	} `yaml:"metrics,omitempty"`
}

var (
	yam    Data
	eat    = flag.Bool("eat", false, "Eat matching lines.")
	debug  = flag.Bool("debug", false, "Display more of the inner workings.")
	config = flag.String("config", "metrics.yml", "Config file.")

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
)

func main() {

	flag.Parse()

	data, err := ioutil.ReadFile(*config)
	if err != nil {
		panic(err.Error())
	}

	err = yaml.Unmarshal(data, &yam)
	if err != nil {
		panic(err.Error())
	}

	for index, metric := range yam.Metrics {

		metricName := fmt.Sprintf("%s_%s", yam.Basename, metric.Name)

		if metric.PromType == "gauge" {
			yam.Metrics[index].Collector = prometheus.NewGauge(
				prometheus.GaugeOpts{
					Name: metricName,
					Help: metric.Description,
				})

		} else {

			yam.Metrics[index].Collector = prometheus.NewGauge(
				prometheus.GaugeOpts{
					Name: metricName,
					Help: metric.Description,
				})
		}
		prometheus.MustRegister(yam.Metrics[index].Collector)
		yam.Metrics[index].Compiled = regexp.MustCompile(metric.Regex)
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

	//
	// Start up the metrics endpoint
	//
	http.Handle("/metrics", prometheus.Handler())
	go http.ListenAndServe(":9000", nil)

	//
	// read from stdin
	//  - check against each regex
	//  - update metric on match
	//  - eat/filter output
	//
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := scanner.Text()

		totalLines.Inc()
		bytesRead.Add(float64(len(line)))

		for _, metric := range yam.Metrics {
			if *debug {
				fmt.Printf("Testing against %s\n", metric.Name)
			}

			result := metric.Compiled.FindStringSubmatch(line)

			if result != nil {

				matchedLines.Inc()

				if metric.PromType == "gauge" {
					if s, err := strconv.ParseFloat(result[1], 64); err == nil {
						metric.Collector.(prometheus.Gauge).Set(s)
					}
				} else {
					metric.Collector.(prometheus.Counter).Inc()
				}

				if *debug {
					fmt.Printf(" ** Match **\n")
				}
			}
		}

		if *eat == false {
			fmt.Println(line)
		}

	}

}
