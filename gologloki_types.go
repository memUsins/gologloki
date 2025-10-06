package gologloki

import (
	"github.com/memUsins/golog"
	"time"
)

type LokiAdapter interface {
	golog.Adapter
}

// lokiLogEntry loki log struct
type lokiLogEntry struct {
	Timestamp time.Time         `json:"timestamp"`
	Line      string            `json:"line"`
	Level     string            `json:"level"`
	Labels    map[string]string `json:"labels"`
}

// lokiStream loki stream struct
type lokiStream struct {
	Stream map[string]string `json:"stream"`
	Values [][2]string       `json:"values"`
}

// lokiPayload loki payload struct
type lokiPayload struct {
	Streams []lokiStream `json:"streams"`
}
