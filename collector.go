/*******************************************************************************
*
* Copyright 2017 SAP SE
* Copyright 2015 The Prometheus Authors
*
* Licensed under the Apache License, Version 2.0 (the "License");
* you may not use this file except in compliance with the License.
* You should have received a copy of the License along with this
* program. If not, you may obtain a copy of the License at
*
*     http://www.apache.org/licenses/LICENSE-2.0
*
* Unless required by applicable law or agreed to in writing, software
* distributed under the License is distributed on an "AS IS" BASIS,
* WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
* See the License for the specific language governing permissions and
* limitations under the License.
*
*******************************************************************************/

package main

import (
	"fmt"
	"sort"
	"time"

	"github.com/beevik/ntp"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/log"
)

var (
	serverIsUp = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "ntp",
		Name:      "server_is_up",
		Help:      "Ntp server is functionnal or not.",
	})
	drift = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "ntp",
		Name:      "drift_seconds",
		Help:      "Difference between system time and NTP time.",
	}, []string{"server"})
	stratum = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "ntp",
		Name:      "stratum",
		Help:      "Stratum of NTP server.",
	})
	scrapeDuration = prometheus.NewSummary(prometheus.SummaryOpts{
		Namespace: "ntp",
		Name:      "scrape_duration_seconds",
		Help:      "ntp_exporter: Duration of a scrape job.",
	})
)

//Collector implements the prometheus.Collector interface.
type Collector struct {
	NtpServer              string
	NtpProtocolVersion     int
	NtpMeasurementDuration time.Duration
}

//Describe implements the prometheus.Collector interface.
func (c Collector) Describe(ch chan<- *prometheus.Desc) {
	serverIsUp.Describe(ch)
	drift.Describe(ch)
	stratum.Describe(ch)
	scrapeDuration.Describe(ch)
}

//Collect implements the prometheus.Collector interface.
func (c Collector) Collect(ch chan<- prometheus.Metric) {
	err := c.measure()
	//only report data when measurement was successful
	if err == nil {
		serverIsUp.Collect(ch)
		drift.Collect(ch)
		stratum.Collect(ch)
		scrapeDuration.Collect(ch)
	} else {
		serverIsUp.Collect(ch)
		log.Errorln(err)
		return
	}
}

func (c Collector) measure() error {
	const highDrift = 0.01

	begin := time.Now()
	clockOffset, strat, err := c.getClockOffsetAndStratum()

	if err != nil {
		serverIsUp.Set(0)
		return fmt.Errorf("couldn't get NTP drift: %s", err)
	}

	//if clock drift is unusually high (e.g. >10ms): repeat measurements for 30 seconds and submit median value
	if clockOffset > highDrift {
		var measurementsClockOffset []float64
		var measurementsStratum []float64

		log.Warnf("clock drift is above %.2fs, taking multiple measurements for %.2f seconds", highDrift, c.NtpMeasurementDuration.Seconds())
		for time.Since(begin) < c.NtpMeasurementDuration {
			clockOffset, stratum, err := c.getClockOffsetAndStratum()

			if err != nil {
				serverIsUp.Set(0)
				return fmt.Errorf("couldn't get NTP drift: %s", err)
			}

			measurementsClockOffset = append(measurementsClockOffset, clockOffset)
			measurementsStratum = append(measurementsStratum, stratum)

		}

		clockOffset = calculateMedian(measurementsClockOffset)
		strat = calculateMedian(measurementsStratum)
	}

	drift.WithLabelValues(c.NtpServer).Set(clockOffset)
	stratum.Set(strat)
	serverIsUp.Set(1)
	scrapeDuration.Observe(time.Since(begin).Seconds())
	return nil
}

func (c Collector) getClockOffsetAndStratum() (clockOffset float64, strat float64, err error) {
	options := ntp.QueryOptions{Version: c.NtpProtocolVersion}
	resp, err := ntp.QueryWithOptions(c.NtpServer, options)
	if err != nil {
		serverIsUp.Set(0)
		return 0, 0, fmt.Errorf("couldn't get NTP drift: %s", err)
	}
	clockOffset = resp.ClockOffset.Seconds()
	strat = float64(resp.Stratum)
	return clockOffset, strat, nil
}

func calculateMedian(slice []float64) (median float64) {

	sort.Float64s(slice)

	middle := len(slice) / 2
	median = slice[middle]
	if len(slice)%2 == 0 {
		median = (median + slice[middle-1]) / 2
	}
	return median
}
