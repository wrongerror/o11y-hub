package metrics

import (
	"log/slog"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/wrongerror/o11y-hub/pkg/expire"
)

func plog() *slog.Logger {
	return slog.With("component", "metrics.Expirer")
}

// Expirer drops metrics from labels that haven't been updated during a given timeout
type Expirer[T prometheus.Metric] struct {
	entries *expire.ExpiryMap[*MetricEntry[T]]
	wrapped prometheus.Collector
}

type MetricEntry[T prometheus.Metric] struct {
	Metric    T
	labelVals []string
}

// NewExpirer creates a metric that wraps a given Collector. Its labeled instances are dropped
// if they haven't been updated during the last timeout period
func NewExpirer[T prometheus.Metric](wrapped prometheus.Collector, clock expire.Clock, expireTime time.Duration) *Expirer[T] {
	return &Expirer[T]{
		wrapped: wrapped,
		entries: expire.NewExpiryMap[*MetricEntry[T]](clock, expireTime),
	}
}

// WithLabelValues returns the Metric for the given slice of label
// values (same order as the variable labels in Desc). If that combination of
// label values is accessed for the first time, a new Metric is created.
// If not, a cached copy is returned and the "last access" cache time is updated.
func (ex *Expirer[T]) WithLabelValues(lbls ...string) *MetricEntry[T] {
	return ex.entries.GetOrCreate(lbls, func() *MetricEntry[T] {
		plog().With("labelValues", lbls).Debug("storing new metric label set")

		// Try to get metric with label values from the wrapped collector
		var metric T
		if metricVec, ok := ex.wrapped.(*prometheus.HistogramVec); ok {
			c, err := metricVec.GetMetricWithLabelValues(lbls...)
			if err != nil {
				panic(err)
			}
			metric = c.(T)
		} else if metricVec, ok := ex.wrapped.(*prometheus.CounterVec); ok {
			c, err := metricVec.GetMetricWithLabelValues(lbls...)
			if err != nil {
				panic(err)
			}
			metric = c.(T)
		} else if metricVec, ok := ex.wrapped.(*prometheus.GaugeVec); ok {
			c, err := metricVec.GetMetricWithLabelValues(lbls...)
			if err != nil {
				panic(err)
			}
			metric = c.(T)
		} else {
			panic("unsupported metric type")
		}

		return &MetricEntry[T]{
			Metric:    metric,
			labelVals: lbls,
		}
	})
}

// Describe wraps prometheus.Collector Describe method
func (ex *Expirer[T]) Describe(descs chan<- *prometheus.Desc) {
	ex.wrapped.Describe(descs)
}

// Collect wraps prometheus.Collector Collect method
func (ex *Expirer[T]) Collect(metrics chan<- prometheus.Metric) {
	log := plog()
	for _, old := range ex.entries.DeleteExpired() {
		// Delete from the underlying MetricVec
		if metricVec, ok := ex.wrapped.(*prometheus.HistogramVec); ok {
			metricVec.DeleteLabelValues(old.labelVals...)
		} else if metricVec, ok := ex.wrapped.(*prometheus.CounterVec); ok {
			metricVec.DeleteLabelValues(old.labelVals...)
		} else if metricVec, ok := ex.wrapped.(*prometheus.GaugeVec); ok {
			metricVec.DeleteLabelValues(old.labelVals...)
		}
		log.With("labelValues", old.labelVals).Debug("deleting old Prometheus metric")
	}
	for _, m := range ex.entries.All() {
		metrics <- m.Metric
	}
}
