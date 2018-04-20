// Takes your stdout and puts it into Prometheus!
package main

import (
	"bufio"
	"errors"
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
	"time"
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
		Name        string   `yaml:"name,omitempty"`
		Description string   `yaml:"description,omitempty"`
		Regex       string   `yaml:"regex,omitempty"`
		Value       string   `yaml:"value,omitempty"`
		Labels      []string `yaml:"labels,omitempty"`
		Collector   prometheus.Collector
		Compiled    *regexp.Regexp
		GroupName   []string
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
	tardy      = flag.Int("tardy", 0, "Hang around for X seconds after stdin closes")

	labels prometheus.Labels
	value  float64

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
		log.Fatalf("Failed to open config file, %v", err)
	}

	err = yaml.Unmarshal(data, &cnf)
	if err != nil {
		log.Fatalf("Failed to parse YAML file, %v", err)
	}

	for index, metric := range cnf.Metrics {

		metricName := cnf.Basename + "_" + metric.Name
		cnf.Metrics[index].Compiled = regexp.MustCompile(metric.Regex)
		cnf.Metrics[index].GroupName = cnf.Metrics[index].Compiled.SubexpNames()

		if *debug {
			log.Printf("Added metric for %s\n", metricName)
		}
		if metric.Value != "" {

			// metrics that have labels
			if len(metric.Labels) > 0 {
				cnf.Metrics[index].Collector = prometheus.NewGaugeVec(
					prometheus.GaugeOpts{
						Name: metricName,
						Help: metric.Description,
					},
					metric.Labels,
				)
				if *debug {
					log.Println("   Type GaugeVec")
				}

			} else {
				cnf.Metrics[index].Collector = prometheus.NewGauge(
					prometheus.GaugeOpts{
						Name: metricName,
						Help: metric.Description,
					})
				if *debug {
					log.Println("   Type Gauge")
				}
			}

		} else {

			if len(metric.Labels) > 0 {
				cnf.Metrics[index].Collector = prometheus.NewCounterVec(
					prometheus.CounterOpts{
						Name: metricName,
						Help: metric.Description,
					},
					metric.Labels,
				)
				if *debug {
					log.Println("   Type CounterVec")
				}
			} else {
				cnf.Metrics[index].Collector = prometheus.NewCounter(
					prometheus.CounterOpts{
						Name: metricName,
						Help: metric.Description,
					})
				if *debug {
					log.Println("   Type Counter")
				}
			}
		}

		prometheus.MustRegister(cnf.Metrics[index].Collector)

		if *debug {
			log.Printf("   Value group name is %s\n", cnf.Metrics[index].Value)
			log.Printf("   Labels are %v\n", cnf.Metrics[index].Labels)
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
				log.Printf("Testing against metric [%s]\n", metric.Name)
			}

			//
			// There are two types of metric
			// Gauge - goes up and down.
			// Counter - goes up or down.
			//
			// Either can have labels attached
			//

			result := metric.Compiled.FindStringSubmatch(line)

			if len(result) != 0 {

				matchedLines.Inc()
				matchFound = true
				if *debug {
					log.Printf(" ** Match **\n")
				}

				//
				// If we named our value, then search through
				// the results for it.
				//
				if metric.Value != "" {
					value, err = getValue(metric.Value,
						metric.GroupName,
						result)
					if err != nil {
						badFloats.Inc()
						continue
					}
					if *debug {
						log.Printf("Value = %.4f\n", value)
					}
				}

				//
				// If we have labels to attach, search through
				// the results and create a prometheus.Labels
				// structure.
				//
				if len(metric.Labels) > 0 {
					labels, err = getLabels(metric.Labels,
						metric.GroupName,
						result)
					if err != nil {
						log.Println("problems finding labels")
					}
				}

				//
				// There is probably some coolkid golang way to
				// this...
				//
				if metric.Value == "" {
					// counter
					if len(metric.Labels) > 0 {
						// counter + labels
						metric.Collector.(*prometheus.CounterVec).With(labels).Inc()
						if *debug {
							log.Printf("CounterVecLabels.Inc() [%+v]\n",
								labels)
						}
					} else {
						// counter
						metric.Collector.(prometheus.Counter).Inc()
						if *debug {
							log.Printf("CounterVec.Inc()\n")
						}
					}
				} else {
					// gauge
					if len(metric.Labels) > 0 {
						// gauge + labels + values
						metric.Collector.(*prometheus.GaugeVec).With(labels).Set(value)
						if *debug {
							log.Printf("GaugeVecLabels.Set(%.4f) [%+v]\n", value, labels)
						}
					} else {
						// gauge + values
						metric.Collector.(prometheus.Gauge).Set(value)
						if *debug {
							log.Printf("GaugeVec.Set(%.4f)\n", value, labels)
						}
					}

				}
			} // for metrics

		} // len(result) != 0

		if cnf.EatAll {
			continue
		}
		if matchFound && cnf.EatMatches {
			continue
		}
		fmt.Println(line)

	} // for scanner

	if *tardy != 0 {
		log.Printf("Stdin closed, waiting %d seconds", *tardy)
		time.Sleep(time.Duration(*tardy*1000) * time.Millisecond)
	}

}

func getValue(valueName string,
	groupNames []string,
	results []string) (float64, error) {
	//
	// find the index of this value in the list of groups
	//
	idx := indexOf(valueName, groupNames)

	//
	// grab it from the results, convert it to a float
	//
	value, err := strconv.ParseFloat(results[idx], 64)

	if err != nil {
		return 0.0, err
	}
	return value, nil
}

func getLabels(labelNames []string,
	groupNames []string,
	results []string) (prometheus.Labels, error) {

	value := prometheus.Labels{}

	for _, labelName := range labelNames {
		//
		// find the index of this label in the list of groups
		//
		idx := indexOf(labelName, groupNames)
		if idx == -1 {
			return nil, errors.New("couldn't find label in results")
		}

		//
		// grab it from the results, bung it in the value struct
		//
		value[labelName] = results[idx]
	}

	return value, nil
}

func indexOf(word string, data []string) int {
	for k, v := range data {
		if word == v {
			return k
		}
	}
	return -1
}
