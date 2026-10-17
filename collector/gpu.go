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

package collector

import (
	"bytes"
	"log/slog"
	"strconv"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
)

const gpuSubsystem = "gpu"

// getGpuComponents returns the vcgencmd clock components that are
// considered part of the GPU for frequency metrics.
func getGpuComponents() []string {
	return []string{"core", "h264", "v3d", "isp", "h264-qp", "hevc", "hevc-qp"}
}

type gpuCollector struct {
	vcgencmd       string
	transport string
	gpuTempCelsius *prometheus.Desc
	gpuFreqHertz   *prometheus.Desc
	gpuVoltage     *prometheus.Desc
}

func init() {
	registerCollector("gpu", defaultEnabled, NewGPUCollector)
}

// NewGPUCollector returns a new Collector exposing GPU temperature, frequency and
// voltage metrics sourced from vcgencmd.
func NewGPUCollector() (Collector, error) {
	gc := &gpuCollector{
		vcgencmd: *vcgencmdPath,
		transport: *firmwareTransport,
		gpuTempCelsius: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, gpuSubsystem, "temperature_celsius"),
			"GPU temperature in degrees celsius (°C).",
			nil, nil,
		),
		gpuFreqHertz: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, gpuSubsystem, "frequency_hertz"),
			"GPU clock frequency in hertz (Hz).",
			[]string{"component"}, nil,
		),
		gpuVoltage: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, gpuSubsystem, "voltage_volts"),
			"GPU core voltage in volts.",
			nil, nil,
		),
	}
	return gc, nil
}

// Update implements the Collector interface.
func (c *gpuCollector) Update(ch chan<- prometheus.Metric) error {
	// GPU temperature. Same thermal sensor as cpuTemp on Pi but the
	// firmware presents it via vcgencmd measure_temp. Useful when the
	// /sys/class/thermal device isn't readable.
	temp, err := c.measureTemp()
	if err != nil {
		return err
	}
	ch <- prometheus.MustNewConstMetric(
		c.gpuTempCelsius,
		prometheus.GaugeValue, temp,
	)

	// Per-component clock frequencies.
	for _, component := range getGpuComponents() {
		freq, err := c.measureClock(component)
		if err != nil {
			// Component clocks are not present on every Pi. Log and continue.
			slog.Debug("skipping gpu component", "component", component, "err", err)
			continue
		}
		ch <- prometheus.MustNewConstMetric(
			c.gpuFreqHertz,
			prometheus.GaugeValue, freq, component,
		)
	}

	// GPU core voltage. Reported by vcgencmd measure_volts.
	volts, err := c.measureVolts()
	if err != nil {
		slog.Debug("measure_volts failed", "err", err)
	} else {
		ch <- prometheus.MustNewConstMetric(
			c.gpuVoltage,
			prometheus.GaugeValue, volts,
		)
	}

	return nil
}

// measureTemp runs `vcgencmd measure_temp` and returns the temperature
// in degrees celsius.
func (c *gpuCollector) measureTemp() (float64, error) {
	out, err := firmwareCommand(c.transport, c.vcgencmd, "measure_temp")
	if err != nil {
		return 0, err
	}
	return parseValue(string(out), "temp=", "'C")
}

// measureVolts runs `vcgencmd measure_volts` and returns the voltage
// in volts.
func (c *gpuCollector) measureVolts() (float64, error) {
	out, err := firmwareCommand(c.transport, c.vcgencmd, "measure_volts")
	if err != nil {
		return 0, err
	}
	return parseValue(string(out), "volt=", "V")
}

// measureClock runs `vcgencmd measure_clock <component>` and returns
// the clock frequency in hertz.
func (c *gpuCollector) measureClock(component string) (float64, error) {
	out, err := firmwareCommand(c.transport, c.vcgencmd, "measure_clock "+component)
	if err != nil {
		return 0, err
	}
	// Example output: "frequency(0)=400000000"
	return parseValue(string(out), "=", "")
}

// parseValue expects input of the form `<prefix><value><suffix>` and
// parses the float between them.
func parseValue(input, prefix, suffix string) (float64, error) {
	s := input
	if prefix != "" {
		idx := strings.Index(s, prefix)
		if idx < 0 {
			return 0, &parseError{input: input}
		}
		s = s[idx+len(prefix):]
	}
	if suffix != "" {
		s = strings.TrimSuffix(s, suffix)
	}
	s = string(bytes.TrimSpace([]byte(s)))
	return strconv.ParseFloat(s, 64)
}

type parseError struct{ input string }

func (e *parseError) Error() string { return "unexpected vcgencmd output: " + e.input }
