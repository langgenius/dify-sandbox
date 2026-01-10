package metrics

import (
    "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/promauto"
)

var (
    // Total runs by language and result (success|error)
    RunsTotal = promauto.NewCounterVec(
        prometheus.CounterOpts{
            Name: "sandbox_runs_total",
            Help: "Total number of sandbox code executions.",
        },
        []string{"language", "result"},
    )

    // Duration of runs by language and result
    RunDurationSeconds = promauto.NewHistogramVec(
        prometheus.HistogramOpts{
            Name:    "sandbox_run_duration_seconds",
            Help:    "Duration of sandbox code executions in seconds.",
            Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 2, 3, 5, 10, 20, 30},
        },
        []string{"language", "result"},
    )

    // In-flight runs by language
    InflightRuns = promauto.NewGaugeVec(
        prometheus.GaugeOpts{
            Name: "sandbox_runs_inflight",
            Help: "Current number of in-flight sandbox code executions.",
        },
        []string{"language"},
    )

    // HTTP layer: in-flight requests and rejections at the limiter
    RequestsInFlight = promauto.NewGauge(
        prometheus.GaugeOpts{
            Name: "sandbox_requests_inflight",
            Help: "In-flight requests to the sandbox run API.",
        },
    )

    RequestsRejectedTotal = promauto.NewCounter(
        prometheus.CounterOpts{
            Name: "sandbox_requests_rejected_total",
            Help: "Total number of requests rejected due to max request limits.",
        },
    )

    // Worker semaphore usage
    WorkersInUse = promauto.NewGauge(
        prometheus.GaugeOpts{
            Name: "sandbox_workers_in_use",
            Help: "Current number of workers acquired by the worker semaphore.",
        },
    )
)