stdout2prom

Much to the dismay of the Prometheus purists, this application applies arbitrary regular expressions against the STDOUT and creates metrics. I bear no responsibility for the level of anger in your Prometheus admin.

What/How does it do it?
stdout2prom reads everything from STDOUT, processes each line with whatever regular expressions you have designed and uses matches to increment counters. It spawns a http endpoint called /metrics that contains these metrics. Point your prometheus at this endpoint.

The regular expressions make use of named subgroups and translate these into labels on the metric. This allows you to generate more complicated metrics, ie counting the number of 200 vs 404 HTTP return codes.

Metrics and their regular expressions are defined in a YAML configuration file, below is an simple example:

```
basename: "myMetrics"
eatMatches: false
eatAll: false
listen: ":9000"

metrics:
  - name: "post"
    description: "Post times of input packets"
    regex: '.*POST\s+.*\s+(?P<returncode>\d+)\s+(?P<response>\d+)ms'
    value: "response"
    labels:
      - "returncode"

  - name: "packetsOut"
    regex: "output packet"
    description: "Count of the output packets"
```

Some of the fields might need a little more explanation:

- basename: This is prefixed to each metric name
- eatMatches: If a line matches, then don't replicate it to STDOUT.
- eatAll: If this is true, then don't replicate any lines to STDOUT.
- listen: HTTP endpoint

For each metric you define, there are the following options:
- name: your metric will be called this prefixed with the basename from above
- description: something that describes your metrics
- regex: a regular expression
- value: Takes the matching named subgroup and makes it the VALUE of this metrics
- labels: A list of labels to apply to this metric, these should have matching named subgroups.


Command line options

```
  -config string
    	Config file. (default "metrics.yml")
  -cpuprofile string
    	write cpu profile to file
  -debug
    	Display more of the inner workings.
  -tardy int
    	Hang around for X seconds after stdin closes
```
