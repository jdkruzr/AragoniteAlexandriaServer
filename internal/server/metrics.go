package server

import "github.com/prometheus/client_golang/prometheus"

type Metrics struct {
	Requests *prometheus.CounterVec
	Duration *prometheus.HistogramVec
}

func NewMetrics(registerer prometheus.Registerer) *Metrics {
	m := &Metrics{
		Requests: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "alexandria_http_requests_total", Help: "HTTP requests handled by Alexandria."}, []string{"listener", "method", "route", "status"}),
		Duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "alexandria_http_request_duration_seconds", Help: "HTTP request duration.", Buckets: prometheus.DefBuckets}, []string{"listener", "method", "route"}),
	}
	registerer.MustRegister(m.Requests, m.Duration)
	return m
}
