package metric

import (
	"context"
	"log/slog"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

var (
	// GRPCServerRequestsTotal counts the total number of gRPC requests by method and status code
	GRPCServerRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "grpc",
			Name:      "server_requests_total",
			Help:      "Total number of gRPC requests by method and status code.",
		},
		[]string{"method", "status_code"},
	)

	// GRPCServerRequestDuration measures the duration of gRPC requests in seconds
	GRPCServerRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: "grpc",
			Name:      "server_request_duration_seconds",
			Help:      "Duration of gRPC requests in seconds.",
			Buckets:   prometheus.DefBuckets,
		},
		[]string{"method", "status_code"},
	)
)

// MetricsUnaryServerInterceptor returns a new unary server interceptor that collects metrics for gRPC requests
func MetricsUnaryServerInterceptor() grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		startTime := time.Now()

		// Call the handler
		resp, err := handler(ctx, req)

		// Record metrics
		duration := time.Since(startTime).Seconds()
		statusCode := status.Code(err).String()
		method := info.FullMethod

		GRPCServerRequestsTotal.WithLabelValues(method, statusCode).Inc()
		GRPCServerRequestDuration.WithLabelValues(method, statusCode).Observe(duration)

		return resp, err
	}
}

// LoggingUnaryServerInterceptor returns a new unary server interceptor that logs gRPC requests and responses
func LoggingUnaryServerInterceptor() grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		startTime := time.Now()
		method := info.FullMethod

		// Log request
		slog.InfoContext(ctx, "gRPC request started",
			"method", method,
			"request", req,
		)

		// Call the handler
		resp, err := handler(ctx, req)

		// Calculate duration
		duration := time.Since(startTime)
		statusCode := status.Code(err)

		// Log response
		if err != nil {
			// Log error with more details
			st, _ := status.FromError(err)
			slog.ErrorContext(ctx, "gRPC request failed",
				"method", method,
				"duration", duration,
				"status_code", statusCode.String(),
				"error", err.Error(),
				"error_details", st.Details(),
				"request", req,
				"response", resp,
			)
		} else {
			slog.InfoContext(ctx, "gRPC request completed",
				"method", method,
				"duration", duration,
				"status_code", statusCode.String(),
				"request", req,
				"response", resp,
			)
		}

		return resp, err
	}
}
