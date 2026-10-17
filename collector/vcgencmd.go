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
	"log/slog"
	"fmt"
	"strconv"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
)

const vcgencmdSubsystem = "raspi"

// throttled register bit values. See:
// https://github.com/raspberrypi/firmware/blob/master/boot/bootcode.c
// and vcgencmd help get_throttled.
//
// current state bits (will clear when the condition clears):
//   0x1   under-voltage
//   0x2   arm frequency capped
//   0x4   currently throttled
//   0x8   soft temperature limit
// sticky bits (set once during the boot, never cleared until reboot):
//   0x10000  under-voltage has occurred
//   0x20000  arm frequency capping has occurred
//   0x40000  throttling has occurred
//   0x80000  soft temperature limit has occurred
var throttledBits = []struct {
	mask    uint32
	label   string
	current bool // true => current-state bit; false => sticky since-boot bit
}{
	{0x1, "under_voltage", true},
	{0x2, "arm_frequency_capped", true},
	{0x4, "throttled", true},
	{0x8, "soft_temperature_limit", true},
	{0x10000, "under_voltage_occurred", false},
	{0x20000, "arm_frequency_capping_occurred", false},
	{0x40000, "throttling_occurred", false},
	{0x80000, "soft_temperature_limit_occurred", false},
}

type vcgencmdCollector struct {
	vcgencmd             string
	transport string
	voltageVolts         *prometheus.Desc
	throttledRaw         *prometheus.Desc
	throttledFlags       *prometheus.Desc
}

func init() {
	registerCollector("vcgencmd", defaultEnabled, NewVcgencmdCollector)
}

// NewVcgencmdCollector returns a new Collector exposing Raspberry Pi
// firmware metrics sourced from vcgencmd: core voltage and the
// throttled register.
func NewVcgencmdCollector() (Collector, error) {
	cc := &vcgencmdCollector{
		vcgencmd: *vcgencmdPath,
		transport: *firmwareTransport,
		voltageVolts: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, vcgencmdSubsystem, "voltage_volts"),
			"SoC core voltage in volts (measure_volts).",
			nil, nil,
		),
		throttledRaw: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, vcgencmdSubsystem, "throttled_raw"),
			"Raw value of the firmware throttled register (hex parsed as int). "+
				"Bitfield described by rpi_raspi_throttled flags.",
			nil, nil,
		),
		throttledFlags: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, vcgencmdSubsystem, "throttled"),
			"Firmware throttled register flags (1 = active). "+
				"`name` is the flag; `kind` is `current` (latched, clears when "+
				"condition clears) or `occurred` (sticky since boot).",
			[]string{"name", "kind"}, nil,
		),
	}
	return cc, nil
}

// Update implements the Collector interface.
func (c *vcgencmdCollector) Update(ch chan<- prometheus.Metric) error {
	volts, err := c.measureVolts()
	if err != nil {
		slog.Debug("measure_volts failed", "err", err)
	} else {
		ch <- prometheus.MustNewConstMetric(
			c.voltageVolts,
			prometheus.GaugeValue, volts,
		)
	}

	raw, ok, err := c.getThrottled()
	if err != nil {
		slog.Debug("get_throttled failed", "err", err)
		return err
	}
	if !ok {
		return nil
	}

	ch <- prometheus.MustNewConstMetric(
		c.throttledRaw,
		prometheus.GaugeValue, float64(raw),
	)

	for _, bit := range throttledBits {
		kind := "occurred"
		if bit.current {
			kind = "current"
		}
		var val float64
		if raw&bit.mask != 0 {
			val = 1
		}
		ch <- prometheus.MustNewConstMetric(
			c.throttledFlags,
			prometheus.GaugeValue, val, bit.label, kind,
		)
	}
	return nil
}

// measureVolts calls `vcgencmd measure_volts` and returns the voltage in volts.
func (c *vcgencmdCollector) measureVolts() (float64, error) {
	out, err := firmwareCommand(c.transport, c.vcgencmd, "measure_volts")
	if err != nil {
		return 0, err
	}
	// Examples:
	//   volt=0.8500V
	//   volt=1.3625V
	s := string(out)
	if idx := strings.Index(s, "volt="); idx >= 0 {
		s = s[idx+len("volt="):]
	}
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, "V")
	return strconv.ParseFloat(s, 64)
}

// getThrottled calls `vcgencmd get_throttled` and returns the raw hex value.
// It returns ok=false when the firmware doesn't support the call.
func (c *vcgencmdCollector) getThrottled() (raw uint32, ok bool, err error) {
	out, err := firmwareCommand(c.transport, c.vcgencmd, "get_throttled")
	if err != nil {
		return 0, false, err
	}
	s := strings.TrimSpace(string(out))
	const prefix = "throttled="
	if !strings.HasPrefix(s, prefix) {
		return 0, false, fmt.Errorf("unexpected get_throttled result: %q", s)
	}
	v, err := strconv.ParseUint(s[len(prefix):], 0, 32) // 0 => base auto-detected: 0x.. or 0b..
	if err != nil {
		return 0, false, err
	}
	return uint32(v), true, nil
}
