package collector

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
)

// Namespace defines the common namespace for all metrics
const Namespace = "o11y_hub"

var (
	scrapeDurationDesc = prometheus.NewDesc(
		prometheus.BuildFQName(Namespace, "scrape", "collector_duration_seconds"),
		"o11y_hub: Duration of a collector scrape.",
		[]string{"collector"},
		nil,
	)
	scrapeSuccessDesc = prometheus.NewDesc(
		prometheus.BuildFQName(Namespace, "scrape", "collector_success"),
		"o11y_hub: Whether a collector succeeded.",
		[]string{"collector"},
		nil,
	)
	scrapeLastTimeDesc = prometheus.NewDesc(
		prometheus.BuildFQName(Namespace, "scrape", "last_scrape_timestamp_seconds"),
		"o11y_hub: Timestamp of the last scrape.",
		[]string{"collector"},
		nil,
	)
)

var (
	factories              = make(map[string]func(logger *logrus.Logger, config Config) (Collector, error))
	initiatedCollectorsMtx = sync.Mutex{}
	initiatedCollectors    = make(map[string]Collector)
)

// Config represents configuration for collectors
type Config struct {
	// Vizier/Pixie configuration
	VizierAddress    string
	VizierClusterID  string
	VizierJWTKey     string
	VizierJWTService string

	// TLS configuration
	TLSEnabled    bool
	TLSSkipVerify bool
	TLSCertFile   string
	TLSKeyFile    string
	TLSCAFile     string

	// Kubernetes configuration
	KubeconfigPath string

	// Beyla configuration
	BeylaAddress string
	BeylaEnabled bool

	// Logging configuration
	LogDirectory         string
	LogMaxFiles          int
	LogJSONFormat        bool
	LogMaxFileSize       int64
	EnableHTTPTraffic    bool
	EnableNetworkTraffic bool

	// General configuration
	ScrapeTimeout  time.Duration
	MaxConcurrency int

	// Enabled collectors
	EnabledCollectors []string
}

// RegisterCollector registers a new collector with the registry
func RegisterCollector(name string, factory func(logger *logrus.Logger, config Config) (Collector, error)) {
	factories[name] = factory
}

// ObservoCollector implements the prometheus.Collector interface
type ObservoCollector struct {
	Collectors map[string]Collector
	logger     *logrus.Logger
	config     Config
}

// NewObservoCollector creates a new ObservoCollector
func NewObservoCollector(logger *logrus.Logger, config Config) (*ObservoCollector, error) {
	collectors := make(map[string]Collector)
	initiatedCollectorsMtx.Lock()
	defer initiatedCollectorsMtx.Unlock()

	// If no collectors specified, enable all registered collectors
	enabledCollectors := config.EnabledCollectors
	if len(enabledCollectors) == 0 {
		for name := range factories {
			enabledCollectors = append(enabledCollectors, name)
		}
	}

	for _, name := range enabledCollectors {
		factory, exists := factories[name]
		if !exists {
			return nil, fmt.Errorf("collector %s not found", name)
		}

		if collector, ok := initiatedCollectors[name]; ok {
			collectors[name] = collector
		} else {
			collector, err := factory(logger, config)
			if err != nil {
				return nil, fmt.Errorf("failed to create collector %s: %w", name, err)
			}
			collectors[name] = collector
			initiatedCollectors[name] = collector
		}
	}

	return &ObservoCollector{
		Collectors: collectors,
		logger:     logger,
		config:     config,
	}, nil
}

// Describe implements the prometheus.Collector interface
func (c ObservoCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- scrapeDurationDesc
	ch <- scrapeSuccessDesc
	ch <- scrapeLastTimeDesc
}

// Collect implements the prometheus.Collector interface
func (c ObservoCollector) Collect(ch chan<- prometheus.Metric) {
	wg := sync.WaitGroup{}
	wg.Add(len(c.Collectors))

	for name, collector := range c.Collectors {
		go func(name string, collector Collector) {
			execute(name, collector, ch, c.logger)
			wg.Done()
		}(name, collector)
	}
	wg.Wait()
}

func execute(name string, c Collector, ch chan<- prometheus.Metric, logger *logrus.Logger) {
	begin := time.Now()
	err := c.Update(ch)
	duration := time.Since(begin)
	var success float64

	if err != nil {
		if IsNoDataError(err) {
			logger.WithFields(logrus.Fields{
				"collector": name,
				"duration":  duration.Seconds(),
				"error":     err,
			}).Debug("Collector returned no data")
		} else {
			logger.WithFields(logrus.Fields{
				"collector": name,
				"duration":  duration.Seconds(),
				"error":     err,
			}).Error("Collector failed")
		}
		success = 0
	} else {
		logger.WithFields(logrus.Fields{
			"collector": name,
			"duration":  duration.Seconds(),
		}).Debug("Collector succeeded")
		success = 1
	}

	ch <- prometheus.MustNewConstMetric(scrapeDurationDesc, prometheus.GaugeValue, duration.Seconds(), name)
	ch <- prometheus.MustNewConstMetric(scrapeSuccessDesc, prometheus.GaugeValue, success, name)
	ch <- prometheus.MustNewConstMetric(scrapeLastTimeDesc, prometheus.GaugeValue, float64(time.Now().Unix()), name)
}

// Collector is the interface a collector has to implement
type Collector interface {
	// Update collects metrics and sends them to the prometheus channel
	Update(ch chan<- prometheus.Metric) error
}

// TypedDesc is a helper for creating prometheus descriptors with types
type TypedDesc struct {
	desc      *prometheus.Desc
	valueType prometheus.ValueType
}

// MustNewConstMetric creates a new constant metric
func (d *TypedDesc) MustNewConstMetric(value float64, labels ...string) prometheus.Metric {
	return prometheus.MustNewConstMetric(d.desc, d.valueType, value, labels...)
}

// NewTypedDesc creates a new TypedDesc
func NewTypedDesc(name, help string, valueType prometheus.ValueType, labels []string) *TypedDesc {
	return &TypedDesc{
		desc: prometheus.NewDesc(
			prometheus.BuildFQName(Namespace, "", name),
			help,
			labels,
			nil,
		),
		valueType: valueType,
	}
}

// ErrNoData indicates the collector found no data to collect, but had no other error
var ErrNoData = errors.New("collector returned no data")

// IsNoDataError checks if an error is a no-data error
func IsNoDataError(err error) bool {
	return err == ErrNoData
}

// CollectorContext provides context and utilities for collectors
type CollectorContext struct {
	Context context.Context
	Logger  *logrus.Logger
	Config  Config
}

// NewCollectorContext creates a new collector context
func NewCollectorContext(ctx context.Context, logger *logrus.Logger, config Config) *CollectorContext {
	return &CollectorContext{
		Context: ctx,
		Logger:  logger,
		Config:  config,
	}
}
