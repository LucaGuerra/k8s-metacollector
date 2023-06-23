// Copyright 2023 The Falco Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package broker

import (
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"

	"github.com/alacuku/k8s-metadata/internal/events"
)

const (
	brokerQueueSubsystem = "broker"
	queueLatencyKey      = "queue_duration_seconds"
	addsKey              = "queue_adds"

	addLabel    = "Add"
	updateLabel = "Update"
	deleteLabel = "Delete"
)

var (
	// latency is a prometheus metric which keeps track of the duration
	// of sending events from collectors to the message broker.
	latency = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Subsystem: brokerQueueSubsystem,
		Name:      queueLatencyKey,
		Help:      "How long in seconds an event stays in the queue before being requested.",
		Buckets:   prometheus.ExponentialBuckets(10e-9, 10, 10),
	}, []string{"name"})

	adds = prometheus.NewCounterVec(prometheus.CounterOpts{
		Subsystem: brokerQueueSubsystem,
		Name:      addsKey,
		Help:      "Total number of events handled by the queue",
	}, []string{"name", "type"})
)

func init() {
	// Register custom metrics with the global prometheus registry.
	ctrlmetrics.Registry.MustRegister(latency)
	ctrlmetrics.Registry.MustRegister(adds)
}

// metrics holds the metrics related to queue. It tracks the number of produced events for each type of events.
// Also tracks the latency of the queue.
type metrics struct {
	sync.Mutex
	addCounter      prometheus.Counter
	updateCounter   prometheus.Counter
	deleteCounter   prometheus.Counter
	latencyObserver prometheus.Observer
	sentTimes       map[interface{}]time.Time
}

// newMetrics returns a new ChannelMetrics ready to be used.
func newMetrics(name string) *metrics {
	// Initialize counters.
	addCounter := adds.WithLabelValues(name, addLabel)
	addCounter.Add(0)
	updateCounter := adds.WithLabelValues(name, updateLabel)
	updateCounter.Add(0)
	deleteCounter := adds.WithLabelValues(name, deleteLabel)
	deleteCounter.Add(0)
	lattencyObserver := latency.WithLabelValues(name)

	return &metrics{
		Mutex:           sync.Mutex{},
		addCounter:      addCounter,
		updateCounter:   updateCounter,
		deleteCounter:   deleteCounter,
		latencyObserver: lattencyObserver,
		sentTimes:       make(map[interface{}]time.Time),
	}
}

// send to be called before adding the item to the queue.
func (m *metrics) send(evt events.Event) {
	if m == nil {
		return
	}
	m.Lock()
	defer m.Unlock()

	switch evt.Type() {
	case events.Added:
		m.addCounter.Inc()
	case events.Modified:
		m.updateCounter.Inc()
	case events.Deleted:
		m.deleteCounter.Inc()
	}

	if _, ok := m.sentTimes[evt]; !ok {
		m.sentTimes[evt] = time.Now()
	}
}

// receive to be called after the item has been pooped from the queue.
func (m *metrics) receive(evt interface{}) {
	if m == nil {
		return
	}
	m.Lock()
	defer m.Unlock()

	if startTime, ok := m.sentTimes[evt]; ok {
		m.latencyObserver.Observe(time.Since(startTime).Seconds())
		delete(m.sentTimes, evt)
	}
}