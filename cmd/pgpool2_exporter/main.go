package main

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/alecthomas/kingpin/v2"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/promlog"
	"github.com/prometheus/common/promlog/flag"
	"github.com/prometheus/common/version"

	exp "github.com/pgpool/pgpool2_exporter"
	"github.com/prometheus/exporter-toolkit/web"
	"github.com/prometheus/exporter-toolkit/web/kingpinflag"
)

func main() {
	promlogConfig := &promlog.Config{}
	toolkitFlags := kingpinflag.AddFlags(kingpin.CommandLine, ":9719")
	metricsPath := kingpin.Flag("web.telemetry-path", "Path under which to expose metrics.").Default("/metrics").String()
	flag.AddFlags(kingpin.CommandLine, promlogConfig)
	kingpin.Version(version.Print("pgpool2_exporter"))
	kingpin.HelpFlag.Short('h')
	kingpin.Parse()

	exp.Logger = promlog.New(promlogConfig)

	var dsn = os.Getenv("DATA_SOURCE_NAME")

	if len(dsn) == 0 {
		var user = os.Getenv("DATA_SOURCE_USER")
		var pass = os.Getenv("DATA_SOURCE_PASS")
		var uri = os.Getenv("DATA_SOURCE_URI")
		ui := url.UserPassword(user, pass).String()

		dsn = "postgresql://" + ui + "@" + uri
	}

	exporter := exp.NewExporter(dsn, exp.Namespace)
	defer func() {
		exporter.DB.Close()
	}()
	prometheus.MustRegister(exporter)

	// Retrieve Pgpool-II version
	v, err := exp.QueryVersion(exporter.DB)
	if err != nil {
		level.Error(exp.Logger).Log("err", err)
	}
	exp.PgpoolSemver = v

	level.Info(exp.Logger).Log("msg", "Starting pgpool2_exporter", "version", version.Info(), "dsn", exp.MaskPassword(dsn))
	level.Info(exp.Logger).Log("msg", "Listening on address", "address", strings.Join(*toolkitFlags.WebListenAddresses, ","))

	http.Handle(*metricsPath, promhttp.Handler())
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(fmt.Sprintf(exp.LandingPage, *metricsPath)))
	})

	server := &http.Server{}
	if err := web.ListenAndServe(server, toolkitFlags, exp.Logger); err != nil {
		level.Error(exp.Logger).Log("err", err)
		os.Exit(1)
	}
}
