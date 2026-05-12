// Package metrics centralizuje Prometheus instrumentaciju za sva tri
// mikroservisa (user-service, bank-service, notification-service).
//
// Sadrži:
//   - NewServerMetrics: kreira i registruje gRPC server metrike (RPC count,
//     latency histogram, in-flight) preko go-grpc-middleware/v2/providers/prometheus.
//   - Handler:          vraća http.Handler koji izlaže /metrics endpoint.
//   - HTTPMiddleware:   instrumentira gRPC-Gateway HTTP saobraćaj (count po
//     status kodu i metodi + duration histogram).
package metrics

import (
	"net/http"

	grpcprom "github.com/grpc-ecosystem/go-grpc-middleware/providers/prometheus"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Gateway HTTP metrike — instanciraju se globalno jer se mere na nivou procesa.
var (
	httpRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_gateway_requests_total",
			Help: "Total number of HTTP requests handled by the gRPC-Gateway.",
		},
		[]string{"code", "method"},
	)

	httpRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_gateway_request_duration_seconds",
			Help:    "HTTP request latency in seconds, observed at the gateway layer.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"code", "method"},
	)
)

// NewServerMetrics kreira gRPC server metrike sa default histogram bucketima
// i registruje ih na prometheus.DefaultRegisterer.
//
// Vraćeni *ServerMetrics nudi UnaryServerInterceptor() / StreamServerInterceptor()
// koji se dodaju u grpc.ChainUnary/StreamInterceptor(...) chain pri startu servera.
// Posle registracije servisa pozvati InitializeMetrics(grpcSrv) da se nuliraju
// counteri za sve poznate metode (čisto za Grafana panele).
func NewServerMetrics() *grpcprom.ServerMetrics {
	sm := grpcprom.NewServerMetrics(
		grpcprom.WithServerHandlingTimeHistogram(),
	)
	prometheus.MustRegister(sm)
	return sm
}

// Handler vraća http.Handler za /metrics endpoint (text/plain Prometheus expo format).
func Handler() http.Handler {
	return promhttp.Handler()
}

// HTTPMiddleware wrap-uje http.Handler i meri svaki zahtev (broj + latency)
// po (code, method) labelama. Koristi se ispred gRPC-Gateway muxa.
func HTTPMiddleware(next http.Handler) http.Handler {
	return promhttp.InstrumentHandlerCounter(httpRequestsTotal,
		promhttp.InstrumentHandlerDuration(httpRequestDuration, next))
}
