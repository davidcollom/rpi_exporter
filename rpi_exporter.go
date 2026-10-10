// Copyright 2019 Lukas Malkmus
// Copyright 2026 David Collom
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/alecthomas/kingpin/v2"
	"github.com/prometheus/client_golang/prometheus"
	promcollectors "github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/version"

	"github.com/davidcollom/rpi_exporter/collector"
)

// A wrapper around http.Handler to handle filtering.
// Caches already used filter combinations.
// Create a new handler using newHandler().
type handler struct {
	unfilteredHandler        http.Handler
	filteredHandlers         map[string]http.Handler
	exporterMetricsRegistry  *prometheus.Registry
	includeExporterMetrics   bool
}

func newHandler(includeExporterMetrics bool) *handler {
	h := &handler{
		filteredHandlers:       make(map[string]http.Handler),
		includeExporterMetrics: includeExporterMetrics,
	}

	// Add default collectors, if they aren't disabled.
	if h.includeExporterMetrics {
		h.exporterMetricsRegistry = prometheus.NewRegistry()
		h.exporterMetricsRegistry.MustRegister(
			promcollectors.NewProcessCollector(promcollectors.ProcessCollectorOpts{}),
			promcollectors.NewGoCollector(),
		)
	}

	unfilteredHandler, err := h.filteredHandler()
	if err != nil {
		panic(fmt.Sprintf("Couldn't create metrics handler: %s", err))
	}

	h.unfilteredHandler = unfilteredHandler
	return h
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	filters := r.URL.Query()["collect[]"]
	sort.Strings(filters)
	slog.Debug("collect query", "filters", filters)

	if len(filters) == 0 {
		h.unfilteredHandler.ServeHTTP(w, r)
		return
	}

	filteredHandler, err := h.filteredHandler(filters...)
	if err != nil {
		slog.Error("couldn't create filtered handler", "err", err)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf("Couldn't create filtered metrics handler: %s", err)))
		return
	}

	filteredHandler.ServeHTTP(w, r)
}

func (h *handler) filteredHandler(filters ...string) (http.Handler, error) {
	if len(filters) == 0 && h.unfilteredHandler != nil {
		return h.unfilteredHandler, nil
	}

	filtersStr := strings.Join(filters, ",")
	if handler := h.filteredHandlers[filtersStr]; handler != nil {
		return handler, nil
	}

	rpiColl, err := collector.New(filters...)
	if err != nil {
		return nil, fmt.Errorf("couldn't create %s", err)
	}

	reg := prometheus.NewRegistry()
	if err := reg.Register(rpiColl); err != nil {
		return nil, fmt.Errorf("couldn't register collector: %s", err)
	}
	reg.MustRegister(buildInfoHandler())

	if h.includeExporterMetrics {
		base := promhttp.HandlerFor(
			prometheus.Gatherers{h.exporterMetricsRegistry, reg},
			promhttp.HandlerOpts{
				ErrorHandling: promhttp.HTTPErrorOnError,
				Registry:      h.exporterMetricsRegistry,
			},
		)
		h.filteredHandlers[filtersStr] = promhttp.InstrumentMetricHandler(h.exporterMetricsRegistry, base)
	} else {
		h.filteredHandlers[filtersStr] = promhttp.HandlerFor(reg, promhttp.HandlerOpts{
			ErrorHandling: promhttp.HTTPErrorOnError,
		})
	}
	return h.filteredHandlers[filtersStr], nil
}

func HealthCheckHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, `{"alive": true}`)
}

// slogErrorWriter adapts the slog default logger to the promhttp.ErrorLog
// interface (an io.Writer-like sink).
type slogErrorWriter struct{}

func (slogErrorWriter) Write(p []byte) (int, error) {
	slog.Error(strings.TrimRight(string(p), "\n"))
	return len(p), nil
}

// buildInfoHandler returns an always-1 metric for rpi_exporter_build_info.
func buildInfoHandler() prometheus.GaugeFunc {
	return prometheus.NewGaugeFunc(
		prometheus.GaugeOpts{
			Name:        "rpi_exporter_build_info",
			Help:        "rpi_exporter build information (always 1).",
			ConstLabels: buildInfoLabels(),
		},
		func() float64 { return 1 },
	)
}

func buildInfoLabels() prometheus.Labels {
	return prometheus.Labels{
		"version":    version.Version,
		"revision":   version.Revision,
		"branch":     version.Branch,
		"go_version": version.GoVersion,
	}
}

func main() {
	var (
		webListenAddress          = kingpin.Flag("web.listen-address", "Address on which to expose metrics and web interface.").Default(":9243").String()
		webMetricsPath            = kingpin.Flag("web.telemetry-path", "Path under which to expose metrics.").Default("/metrics").String()
		webHealthPath             = kingpin.Flag("web.healthcheck-path", "Path under which the exporter exposes its status.").Default("/health").String()
		webDisableExporterMetrics = kingpin.Flag("web.disable-exporter-metrics", "Exclude metrics about the exporter itself (promhttp_*, process_*, go_*).").Bool()
		logLevel                  = kingpin.Flag("log.level", "Log level (debug, info, warn, error).").Default("info").String()
	)

	kingpin.Version(version.Print("rpi_exporter"))
	kingpin.HelpFlag.Short('h')
	kingpin.Parse()

	// Configure slog default logger.
	var lvl slog.Level
	switch strings.ToLower(*logLevel) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn", "warning":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lvl})))

	slog.Info("starting rpi_exporter",
		"version", version.Info(),
		"build_context", version.BuildContext(),
	)

	mux := http.NewServeMux()
	mux.Handle(*webMetricsPath, newHandler(!*webDisableExporterMetrics))
	mux.HandleFunc(*webHealthPath, HealthCheckHandler)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html>
			<head><title>Raspberry Pi Exporter</title></head>
			<body>
			<h1>Raspberry Pi Exporter</h1>
			<p><a href="` + *webMetricsPath + `">Metrics</a></p>
			<p><a href="` + *webHealthPath + `">Exporter health</a></p>
			</body>
			</html>`))
	})

	srv := &http.Server{
		Addr:         *webListenAddress,
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
		ErrorLog:     slog.NewLogLogger(slog.Default().Handler(), slog.LevelError),
	}

	term := make(chan os.Signal, 1)
	defer close(term)
	signal.Notify(term, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(term)

	slog.Info("listening", "address", *webListenAddress)
	webErr := make(chan error)
	defer close(webErr)
	go func() {
		if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			webErr <- err
		}
	}()

	var (
		ctx    context.Context
		cancel context.CancelFunc
	)
	select {
	case <-term:
		slog.Warn("received SIGTERM, exiting gracefully")
		ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			slog.Error("shutdown error", "err", err)
		}
	case err := <-webErr:
		slog.Error("error starting web server, exiting", "err", err)
	}
	slog.Info("exited")
}
