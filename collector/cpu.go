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
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
)

const cpuSubsystem = "cpu"

func trimNewline(b []byte) []byte {
	return bytes.TrimRight(b, "\r\n")
}

type cpuCollector struct {
	cpuTempCelsius *prometheus.Desc
	cpuFreqHertz   *prometheus.Desc
	cpuFreqMin     *prometheus.Desc
	cpuFreqMax     *prometheus.Desc
}

func init() {
	registerCollector("cpu", defaultEnabled, NewCPUCollector)
}

// NewCPUCollector returns a new Collector exposing CPU temperature and frequency metrics.
func NewCPUCollector() (Collector, error) {
	cc := &cpuCollector{
		cpuTempCelsius: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, cpuSubsystem, "temperature_celsius"),
			"CPU temperature in degrees celsius (°C).",
			nil, nil,
		),
		cpuFreqHertz: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, cpuSubsystem, "frequency_hertz"),
			"CPU frequency in hertz (Hz) (current).",
			[]string{"cpu"}, nil,
		),
		cpuFreqMin: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, cpuSubsystem, "frequency_min_hertz"),
			"CPU minimum frequency in hertz (Hz).",
			[]string{"cpu"}, nil,
		),
		cpuFreqMax: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, cpuSubsystem, "frequency_max_hertz"),
			"CPU maximum frequency in hertz (Hz).",
			[]string{"cpu"}, nil,
		),
	}
	return cc, nil
}

// Update implements the Collector interface.
func (c *cpuCollector) Update(ch chan<- prometheus.Metric) error {
	// Temperature from the SoC thermal zone.
	temp, err := readThermalZoneTemp("/sys/class/thermal/thermal_zone0/temp")
	if err == nil {
		ch <- prometheus.MustNewConstMetric(
			c.cpuTempCelsius,
			prometheus.GaugeValue, temp,
		)
	} else {
		return fmt.Errorf("reading thermal_zone0 temp: %w", err)
	}

	// Frequencies per logical CPU.
	cpus, err := filepath.Glob("/sys/devices/system/cpu/cpu[0-9]*")
	if err != nil {
		return fmt.Errorf("globbing /sys/devices/system/cpu: %w", err)
	}

	for i, cpu := range cpus {
		label := strconv.Itoa(i)

		if cur, err := readMHz(cpu + "/cpufreq/scaling_cur_freq"); err == nil {
			ch <- prometheus.MustNewConstMetric(
				c.cpuFreqHertz,
				prometheus.GaugeValue, cur, label,
			)
		}
		if mn, err := readMHz(cpu + "/cpufreq/scaling_min_freq"); err == nil {
			ch <- prometheus.MustNewConstMetric(
				c.cpuFreqMin,
				prometheus.GaugeValue, mn, label,
			)
		}
		if mx, err := readMHz(cpu + "/cpufreq/scaling_max_freq"); err == nil {
			ch <- prometheus.MustNewConstMetric(
				c.cpuFreqMax,
				prometheus.GaugeValue, mx, label,
			)
		}
		_ = cpu
	}

	return nil
}

// readMHz reads a /sys/devices/system/cpu/cpuN/cpufreq/scaling_*_freq
// file whose contents are kHz and returns Hz. Errors are returned to
// the caller.
func readMHz(path string) (float64, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	v, err := strconv.ParseFloat(string(trimNewline(b)), 64)
	if err != nil {
		return 0, err
	}
	return v * 1000, nil
}

// readThermalZoneTemp reads /sys/class/thermal/thermal_zone*/temp which
// is milli-degrees celsius and returns degrees celsius.
func readThermalZoneTemp(path string) (float64, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	v, err := strconv.ParseFloat(string(trimNewline(b)), 64)
	if err != nil {
		return 0, err
	}
	return v / 1000, nil
}
