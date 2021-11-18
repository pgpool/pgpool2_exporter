/*
Copyright (c) 2021 PgPool Global Development Group

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
*/

package main

import (
	"database/sql"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/blang/semver"
	"github.com/go-kit/kit/log/level"
	_ "github.com/lib/pq"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/promlog"
	"github.com/prometheus/common/promlog/flag"
	"github.com/prometheus/common/version"
	"gopkg.in/alecthomas/kingpin.v2"
)

var (
	listenAddress = kingpin.Flag("web.listen-address", "Address on which to expose metrics and web interface.").Default(":9719").String()
	metricsPath   = kingpin.Flag("web.telemetry-path", "Path under which to expose metrics.").Default("/metrics").String()
	logger        = promlog.New(&promlog.Config{})
)

const (
	namespace   = "pgpool2"
	exporter    = "exporter"
	landingPage = `
	<html>
		<head>
			<title>Pgpool-II Exporter</title>
		</head>
		<body>
			<h1>Pgpool-II Exporter</h1>
			<p>
			<a href='%s'>Metrics</a>
			</p>
		</body>
	</html>`
)

// columnUsage should be one of several enum values which describe how a
// queried row is to be converted to a Prometheus metric.
type columnUsage int

// Convert a string to the corresponding columnUsage
func stringTocolumnUsage(s string) (u columnUsage, err error) {
	switch s {
	case "DISCARD":
		u = DISCARD

	case "LABEL":
		u = LABEL

	case "COUNTER":
		u = COUNTER

	case "GAUGE":
		u = GAUGE

	case "MAPPEDMETRIC":
		u = MAPPEDMETRIC

	case "DURATION":
		u = DURATION

	default:
		err = fmt.Errorf("wrong columnUsage given : %s", s)
	}

	return
}

// nolint: golint
const (
	DISCARD      columnUsage = iota // Ignore this column
	LABEL        columnUsage = iota // Use this column as a label
	COUNTER      columnUsage = iota // Use this column as a counter
	GAUGE        columnUsage = iota // Use this column as a gauge
	MAPPEDMETRIC columnUsage = iota // Use this column with the supplied mapping of text values
	DURATION     columnUsage = iota // This column should be interpreted as a text duration (and converted to milliseconds)
)

// Implement the yaml.Unmarshaller interface
func (cu *columnUsage) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var value string
	if err := unmarshal(&value); err != nil {
		return err
	}

	columnUsage, err := stringTocolumnUsage(value)
	if err != nil {
		return err
	}

	*cu = columnUsage
	return nil
}

// Groups metric maps under a shared set of labels
type MetricMapNamespace struct {
	labels         []string             // Label names for this namespace
	columnMappings map[string]MetricMap // Column mappings in this namespace
}

// Stores the prometheus metric description which a given column will be mapped
// to by the collector
type MetricMap struct {
	discard    bool                 // Should metric be discarded during mapping?
	vtype      prometheus.ValueType // Prometheus valuetype
	namespace  string
	desc       *prometheus.Desc                  // Prometheus descriptor
	conversion func(interface{}) (float64, bool) // Conversion function to turn PG result into float64
}

// User-friendly representation of a prometheus descriptor map
type ColumnMapping struct {
	usage       columnUsage `yaml:"usage"`
	description string      `yaml:"description"`
}

// Exporter collects Pgpool-II stats from the given server and exports
// them using the prometheus metrics package.
type Exporter struct {
	dsn          string
	namespace    string
	mutex        sync.RWMutex
	duration     prometheus.Gauge
	up           prometheus.Gauge
	error        prometheus.Gauge
	totalScrapes prometheus.Counter
	metricMap    map[string]MetricMapNamespace
	db           *sql.DB
}

var (
	metricMaps = map[string]map[string]ColumnMapping{
		"pool_nodes": {
			"hostname":          {LABEL, "Backend hostname"},
			"port":              {LABEL, "Backend port"},
			"role":              {LABEL, "Role (primary or standby)"},
			"status":            {GAUGE, "Backend node Status (1 for up or waiting, 0 for down or unused)"},
			"select_cnt":        {GAUGE, "SELECT statement counts issued to each backend"},
			"replication_delay": {GAUGE, "Replication delay"},
		},
		"pool_backend_stats": {
			"hostname":   {LABEL, "Backend hostname"},
			"port":       {LABEL, "Backend port"},
			"role":       {LABEL, "Role (primary or standby)"},
			"status":     {GAUGE, "Backend node Status (1 for up or waiting, 0 for down or unused)"},
			"select_cnt": {GAUGE, "SELECT statement counts issued to each backend"},
			"insert_cnt": {GAUGE, "INSERT statement counts issued to each backend"},
			"update_cnt": {GAUGE, "UPDATE statement counts issued to each backend"},
			"delete_cnt": {GAUGE, "DELETE statement counts issued to each backend"},
			"ddl_cnt":    {GAUGE, "DDL statement counts issued to each backend"},
			"other_cnt":  {GAUGE, "other statement counts issued to each backend"},
			"panic_cnt":  {GAUGE, "Panic message counts returned from backend"},
			"fatal_cnt":  {GAUGE, "Fatal message counts returned from backend)"},
			"error_cnt":  {GAUGE, "Error message counts returned from backend"},
		},
		"pool_health_check_stats": {
			"hostname":            {LABEL, "Backend hostname"},
			"port":                {LABEL, "Backend port"},
			"role":                {LABEL, "Role (primary or standby)"},
			"status":              {GAUGE, "Backend node Status (1 for up or waiting, 0 for down or unused)"},
			"total_count":         {GAUGE, "Number of health check count in total"},
			"success_count":       {GAUGE, "Number of successful health check count in total"},
			"fail_count":          {GAUGE, "Number of failed health check count in total"},
			"skip_count":          {GAUGE, "Number of skipped health check count in total"},
			"retry_count":         {GAUGE, "Number of retried health check count in total"},
			"average_retry_count": {GAUGE, "Number of average retried health check count in a health check session"},
			"max_retry_count":     {GAUGE, "Number of maximum retried health check count in a health check session"},
			"max_duration":        {GAUGE, "Maximum health check duration in Millie seconds"},
			"min_duration":        {GAUGE, "Minimum health check duration in Millie seconds"},
			"average_duration":    {GAUGE, "Average health check duration in Millie seconds"},
		},
		"pool_processes": {
			"pool_pid": {DISCARD, "PID of Pgpool-II child processes"},
			"database": {DISCARD, "Database name of the currently active backend connection"},
		},
		"pool_cache": {
			"cache_hit_ratio":         {GAUGE, "Query cache hit ratio"},
			"num_hash_entries":        {GAUGE, "Number of total hash entries"},
			"used_hash_entries":       {GAUGE, "Number of used hash entries"},
			"num_cache_entries":       {GAUGE, "Number of used cache entries"},
			"used_cache_entries_size": {GAUGE, "Total size of used cache size"},
			"free_cache_entries_size": {GAUGE, "Total size of free cache size"},
		},
	}
)

// Pgpool-II version
var pgpoolVersionRegex = regexp.MustCompile(`^((\d+)(\.\d+)(\.\d+)?)`)
var version42 = semver.MustParse("4.2.0")
var pgpoolSemver semver.Version

func NewExporter(dsn string, namespace string) *Exporter {

	db, err := getDBConn(dsn)

	if err != nil {
		level.Error(logger).Log("err", err)
		os.Exit(1)
	}

	return &Exporter{
		dsn:       dsn,
		namespace: namespace,
		up: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "up",
			Help:      "Whether the Pgpool-II server is up (1 for yes, 0 for no).",
		}),

		duration: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "last_scrape_duration_seconds",
			Help:      "Duration of the last scrape of metrics from Pgpool-II.",
		}),

		totalScrapes: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "scrapes_total",
			Help:      "Total number of times Pgpool-II has been scraped for metrics.",
		}),

		error: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "last_scrape_error",
			Help:      "Whether the last scrape of metrics from Pgpool-II resulted in an error (1 for error, 0 for success).",
		}),
		metricMap: makeDescMap(metricMaps, namespace),
		db:        db,
	}
}

// Query within a namespace mapping and emit metrics. Returns fatal errors if
// the scrape fails, and a slice of errors if they were non-fatal.
func queryNamespaceMapping(ch chan<- prometheus.Metric, db *sql.DB, namespace string, mapping MetricMapNamespace) ([]error, error) {
	query := fmt.Sprintf("SHOW %s;", namespace)

	// Don't fail on a bad scrape of one metric
	rows, err := db.Query(query)
	if err != nil {
		return []error{}, errors.New(fmt.Sprintln("Error running query on database: ", namespace, err))
	}

	defer rows.Close()

	var columnNames []string
	columnNames, err = rows.Columns()
	if err != nil {
		return []error{}, errors.New(fmt.Sprintln("Error retrieving column list for: ", namespace, err))
	}

	// Make a lookup map for the column indices
	var columnIdx = make(map[string]int, len(columnNames))
	for i, n := range columnNames {
		columnIdx[n] = i
	}

	var columnData = make([]interface{}, len(columnNames))
	var scanArgs = make([]interface{}, len(columnNames))
	for i := range columnData {
		scanArgs[i] = &columnData[i]
	}

	nonfatalErrors := []error{}

	// Read from the result of "SHOW pool_processes"
	if namespace == "pool_processes" {
		frontendByUserDb := make(map[string]map[string]int)
		var frontend_total float64
		var frontend_used float64

		for rows.Next() {
			err = rows.Scan(scanArgs...)
			if err != nil {
				return []error{}, errors.New(fmt.Sprintln("Error retrieving rows:", namespace, err))
			}
			frontend_total++
			// Loop over column names to find currently connected backend database
			var valueDatabase string
			var valueUsername string
			for idx, columnName := range columnNames {
				switch columnName {
				case "database":
					valueDatabase, _ = dbToString(columnData[idx])
				case "username":
					valueUsername, _ = dbToString(columnData[idx])
				}
			}
			if len(valueDatabase) > 0 && len(valueUsername) > 0 {
				frontend_used++
				dbCount, ok := frontendByUserDb[valueUsername]
				if !ok {
					dbCount = map[string]int{valueDatabase: 0}
				}
				dbCount[valueDatabase]++
				frontendByUserDb[valueUsername] = dbCount
			}
		}

		variableLabels := []string{"username", "database"}
		for userName, dbs := range frontendByUserDb {
			for dbName, count := range dbs {
				labels := []string{userName, dbName}
				ch <- prometheus.MustNewConstMetric(
					prometheus.NewDesc(prometheus.BuildFQName("pgpool2", "", "frontend_used"), "Number of used child processes", variableLabels, nil),
					prometheus.GaugeValue,
					float64(count),
					labels...,
				)
			}
		}

		// Generate the metric for "pool_processes"
		ch <- prometheus.MustNewConstMetric(
			prometheus.NewDesc(prometheus.BuildFQName("pgpool2", "", "frontend_total"), "Number of total child processed", nil, nil),
			prometheus.GaugeValue,
			frontend_total,
		)
		ch <- prometheus.MustNewConstMetric(
			prometheus.NewDesc(prometheus.BuildFQName("pgpool2", "", "frontend_used_ratio"), "Ratio of child processes to total processes", nil, nil),
			prometheus.GaugeValue,
			frontend_used/frontend_total,
		)

		return nonfatalErrors, nil
	}

	for rows.Next() {
		err = rows.Scan(scanArgs...)
		if err != nil {
			return []error{}, errors.New(fmt.Sprintln("Error retrieving rows:", namespace, err))
		}

		// Get the label values for this row.
		labels := make([]string, len(mapping.labels))
		for idx, label := range mapping.labels {
			labels[idx], _ = dbToString(columnData[columnIdx[label]])
		}

		// Loop over column names, and match to scan data.
		for idx, columnName := range columnNames {
			if metricMapping, ok := mapping.columnMappings[columnName]; ok {
				// Is this a metricy metric?
				if metricMapping.discard {
					continue
				}

				// If status column, convert string to int.
				if columnName == "status" {
					valueString, ok := dbToString(columnData[idx])
					if !ok {
						nonfatalErrors = append(nonfatalErrors, errors.New(fmt.Sprintln("Unexpected error parsing column: ", namespace, columnName, columnData[idx])))
						continue
					}
					value := parseStatusField(valueString)
					// Generate the metric
					ch <- prometheus.MustNewConstMetric(metricMapping.desc, metricMapping.vtype, value, labels...)
					continue
				}

				value, ok := dbToFloat64(columnData[idx])
				if !ok {
					nonfatalErrors = append(nonfatalErrors, errors.New(fmt.Sprintln("Unexpected error parsing column: ", namespace, columnName, columnData[idx])))
					continue
				}
				// Generate the metric
				ch <- prometheus.MustNewConstMetric(metricMapping.desc, metricMapping.vtype, value, labels...)
			}
		}
	}
	return nonfatalErrors, nil
}

// Establish a new DB connection using dsn.
func getDBConn(dsn string) (*sql.DB, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	err = ping(db)
	if err != nil {
		return nil, err
	}

	return db, nil
}

// Connect to Pgpool-II and run "SHOW POOL_VERSION;" to check connection availability.
func ping(db *sql.DB) error {

	rows, err := db.Query("SHOW POOL_VERSION;")
	if err != nil {
		return fmt.Errorf("error connecting to Pgpool-II: %s", err)
	}
	defer rows.Close()

	return nil
}

// Convert database.sql types to float64s for Prometheus consumption. Null types are mapped to NaN. string and []byte
// types are mapped as NaN and !ok
func dbToFloat64(t interface{}) (float64, bool) {
	switch v := t.(type) {
	case int64:
		return float64(v), true
	case float64:
		return v, true
	case time.Time:
		return float64(v.Unix()), true
	case []byte:
		// Try and convert to string and then parse to a float64
		strV := string(v)
		result, err := strconv.ParseFloat(strV, 64)
		if err != nil {
			return math.NaN(), false
		}
		return result, true
	case string:
		result, err := strconv.ParseFloat(v, 64)
		if err != nil {
			level.Error(logger).Log("msg", "Could not parse string", "err", err)
			return math.NaN(), false
		}
		return result, true
	case bool:
		if v {
			return 1.0, true
		}
		return 0.0, true
	case nil:
		return math.NaN(), true
	default:
		return math.NaN(), false
	}
}

// Convert database.sql to string for Prometheus labels. Null types are mapped to empty strings.
func dbToString(t interface{}) (string, bool) {
	switch v := t.(type) {
	case int64:
		return fmt.Sprintf("%v", v), true
	case float64:
		return fmt.Sprintf("%v", v), true
	case time.Time:
		return fmt.Sprintf("%v", v.Unix()), true
	case nil:
		return "", true
	case []byte:
		// Try and convert to string
		return string(v), true
	case string:
		return v, true
	case bool:
		if v {
			return "true", true
		}
		return "false", true
	default:
		return "", false
	}
}

// Convert bool to int.
func parseStatusField(value string) float64 {
	switch value {
	case "true", "up", "waiting":
		return 1.0
	case "false", "unused", "down":
		return 0.0
	}
	return 0.0
}

// Mask user password in DSN
func maskPassword(dsn string) string {
	pDSN, err := url.Parse(dsn)
	if err != nil {
		return "could not parse DATA_SOURCE_NAME"
	}
	// Mask user password in DSN
	if pDSN.User != nil {
		pDSN.User = url.UserPassword(pDSN.User.Username(), "MASKED_PASSWORD")
	}

	return pDSN.String()
}

// Retrieve Pgpool-II version.
func queryVersion(db *sql.DB) (semver.Version, error) {

	level.Debug(logger).Log("msg", "Querying Pgpool-II version")

	versionRows, err := db.Query("SHOW POOL_VERSION;")
	if err != nil {
		return semver.Version{}, errors.New(fmt.Sprintln("Error querying SHOW POOL_VERSION:", err))
	}
	defer versionRows.Close()

	var columnNames []string
	columnNames, err = versionRows.Columns()
	if err != nil {
		return semver.Version{}, errors.New(fmt.Sprintln("Error retrieving column name for version:", err))
	}
	if len(columnNames) != 1 || columnNames[0] != "pool_version" {
		return semver.Version{}, errors.New(fmt.Sprintln("Error returning Pgpool-II version:", err))
	}

	var pgpoolVersion string
	for versionRows.Next() {
		err := versionRows.Scan(&pgpoolVersion)
		if err != nil {
			return semver.Version{}, errors.New(fmt.Sprintln("Error retrieving SHOW POOL_VERSION rows:", err))
		}
	}

	v := pgpoolVersionRegex.FindStringSubmatch(pgpoolVersion)
	if len(v) > 1 {
		level.Debug(logger).Log("pgpool_version", v[1])
		return semver.ParseTolerant(v[1])
	}

	return semver.Version{}, errors.New(fmt.Sprintln("Error retrieving Pgpool-II version:", err))
}

// Iterate through all the namespace mappings in the exporter and run their queries.
func queryNamespaceMappings(ch chan<- prometheus.Metric, db *sql.DB, metricMap map[string]MetricMapNamespace) map[string]error {
	// Return a map of namespace -> errors
	namespaceErrors := make(map[string]error)

	for namespace, mapping := range metricMap {
		// pool_backend_stats and pool_health_check_stats can not be used before 4.1.
		if namespace == "pool_backend_stats" || namespace == "pool_health_check_stats" {
			if pgpoolSemver.LT(version42) {
				continue
			}
		}

		level.Debug(logger).Log("msg", "Querying namespace", "namespace", namespace)
		nonFatalErrors, err := queryNamespaceMapping(ch, db, namespace, mapping)
		// Serious error - a namespace disappeard
		if err != nil {
			namespaceErrors[namespace] = err
			level.Info(logger).Log("msg", "namespace disappeard", "err", err)
		}
		// Non-serious errors - likely version or parsing problems.
		if len(nonFatalErrors) > 0 {
			for _, err := range nonFatalErrors {
				level.Info(logger).Log("msg", "error parsing", "err", err.Error())
			}
		}
	}

	return namespaceErrors
}

// Describe implements prometheus.Collector.
func (e *Exporter) Describe(ch chan<- *prometheus.Desc) {
	// We cannot know in advance what metrics the exporter will generate
	// from Postgres. So we use the poor man's describe method: Run a collect
	// and send the descriptors of all the collected metrics. The problem
	// here is that we need to connect to the Postgres DB. If it is currently
	// unavailable, the descriptors will be incomplete. Since this is a
	// stand-alone exporter and not used as a library within other code
	// implementing additional metrics, the worst that can happen is that we
	// don't detect inconsistent metrics created by this exporter
	// itself. Also, a change in the monitored Postgres instance may change the
	// exported metrics during the runtime of the exporter.

	metricCh := make(chan prometheus.Metric)
	doneCh := make(chan struct{})

	go func() {
		for m := range metricCh {
			ch <- m.Desc()
		}
		close(doneCh)
	}()

	e.Collect(metricCh)
	close(metricCh)
	<-doneCh
}

// Collect implements prometheus.Collector.
func (e *Exporter) Collect(ch chan<- prometheus.Metric) {
	e.scrape(ch)
	ch <- e.duration
	ch <- e.up
	ch <- e.totalScrapes
	ch <- e.error
}

func (e *Exporter) scrape(ch chan<- prometheus.Metric) {
	e.totalScrapes.Inc()
	var err error
	defer func(begun time.Time) {
		e.duration.Set(time.Since(begun).Seconds())
		if err == nil {
			e.error.Set(0)
		} else {
			e.error.Set(1)
		}
	}(time.Now())

	// Check connection availability and close the connection if it fails.
	if err = ping(e.db); err != nil {
		level.Error(logger).Log("msg", "Error pinging Pgpool-II", "err", err)
		if cerr := e.db.Close(); cerr != nil {
			level.Error(logger).Log("msg", "Error while closing non-pinging connection", "err", err)
		}
		level.Info(logger).Log("msg", "Reconnecting to Pgpool-II")
		e.db, err = sql.Open("postgres", e.dsn)
		e.db.SetMaxOpenConns(1)
		e.db.SetMaxIdleConns(1)

		if err = ping(e.db); err != nil {
			level.Error(logger).Log("msg", "Error pinging Pgpool-II", "err", err)
			if cerr := e.db.Close(); cerr != nil {
				level.Error(logger).Log("msg", "Error while closing non-pinging connection", "err", err)
			}
			e.up.Set(0)
			return
		}
	}

	e.up.Set(1)
	e.error.Set(0)

	e.mutex.RLock()
	defer e.mutex.RUnlock()

	errMap := queryNamespaceMappings(ch, e.db, e.metricMap)
	if len(errMap) > 0 {
		level.Error(logger).Log("err", errMap)
		e.error.Set(1)
	}
}

// Turn the MetricMap column mapping into a prometheus descriptor mapping.
func makeDescMap(metricMaps map[string]map[string]ColumnMapping, namespace string) map[string]MetricMapNamespace {
	var metricMap = make(map[string]MetricMapNamespace)

	for metricNamespace, mappings := range metricMaps {
		thisMap := make(map[string]MetricMap)

		// Get the constant labels
		var variableLabels []string
		for columnName, columnMapping := range mappings {
			if columnMapping.usage == LABEL {
				variableLabels = append(variableLabels, columnName)
			}
		}

		for columnName, columnMapping := range mappings {
			// Determine how to convert the column based on its usage.
			switch columnMapping.usage {
			case DISCARD, LABEL:
				thisMap[columnName] = MetricMap{
					discard: true,
					conversion: func(_ interface{}) (float64, bool) {
						return math.NaN(), true
					},
				}
			case COUNTER:
				thisMap[columnName] = MetricMap{
					vtype: prometheus.CounterValue,
					desc:  prometheus.NewDesc(fmt.Sprintf("%s_%s_%s", namespace, metricNamespace, columnName), columnMapping.description, variableLabels, nil),
					conversion: func(in interface{}) (float64, bool) {
						return dbToFloat64(in)
					},
				}
			case GAUGE:
				thisMap[columnName] = MetricMap{
					vtype: prometheus.GaugeValue,
					desc:  prometheus.NewDesc(fmt.Sprintf("%s_%s_%s", namespace, metricNamespace, columnName), columnMapping.description, variableLabels, nil),
					conversion: func(in interface{}) (float64, bool) {
						return dbToFloat64(in)
					},
				}
			}
		}

		metricMap[metricNamespace] = MetricMapNamespace{variableLabels, thisMap}
	}

	return metricMap
}

func main() {
	promlogConfig := &promlog.Config{}
	flag.AddFlags(kingpin.CommandLine, promlogConfig)
	kingpin.Version(version.Print("pgpool2_exporter"))
	kingpin.HelpFlag.Short('h')
	kingpin.Parse()

	dsn := os.Getenv("DATA_SOURCE_NAME")
	exporter := NewExporter(dsn, namespace)
	defer func() {
		exporter.db.Close()
	}()
	prometheus.MustRegister(exporter)

	// Retrieve Pgpool-II version
	v, err := queryVersion(exporter.db)
	if err != nil {
		level.Error(logger).Log("err", err)
	}
	pgpoolSemver = v

	level.Info(logger).Log("msg", "Starting pgpool2_exporter", "version", version.Info(), "dsn", maskPassword(dsn))
	level.Info(logger).Log("msg", "Listening on address", "address", *listenAddress)

	http.Handle(*metricsPath, promhttp.Handler())
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(fmt.Sprintf(landingPage, *metricsPath)))
	})

	if err := http.ListenAndServe(*listenAddress, nil); err != nil {
		level.Error(logger).Log("err", err)
		os.Exit(1)
	}
}
