# gologloki

gologloki — loki adapter for [golog](https://github.com/memUsins/golog)

## Installation

```shell
$ go get github.com/memUsins/gologloki
```

## Usage

```go
// with default config
logger := golog.NewLogger(gologloki.NewDefaultLokiAdapter(url))

// with custom config
logger := golog.NewLogger(gologloki.NewLokiAdapter(&gologloki.LokiConfig{
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
}))
```