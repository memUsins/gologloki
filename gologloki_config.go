package gologloki

import (
	"github.com/memUsins/golog"
	"time"
)

// LokiConfig core config for adapter
type LokiConfig struct {
	Enable bool

	Level golog.Level

	Url    string
	Labels map[string]string

	BatchSize     int
	BatchInterval time.Duration

	RetryCount int
	RetryDelay time.Duration

	Timeout time.Duration
}

// defaultLokiConfig setting up default config
func defaultLokiConfig(url string) *LokiConfig {
	return &LokiConfig{
		Enable: true,
		Level:  golog.DebugLevel,

		Url: url,
		Labels: map[string]string{
			"job": "app_logs",
		},

		BatchSize:     100,
		BatchInterval: 5 * time.Second,

		RetryCount: 3,
		RetryDelay: 1 * time.Second,

		Timeout: 10 * time.Second,
	}
}
