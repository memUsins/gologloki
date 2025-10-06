package gologloki

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/memUsins/golog"
	"net/http"
	"sync"
	"time"
)

// lokiAdapter implements LokiAdapter
type lokiAdapter struct {
	cfg *LokiConfig

	client *http.Client

	queue chan lokiLogEntry
	quit  chan struct{}
	wg    sync.WaitGroup

	bufferMutex sync.Mutex
	buffer      []lokiLogEntry

	lastFlush time.Time
}

// Log to loki
func (a *lokiAdapter) Log(log golog.Log) {
	if !a.cfg.Enable || !a.cfg.Level.IsEnabled(log.Level) {
		return
	}

	a.Format(&log)

	entry := lokiLogEntry{
		Timestamp: log.Timestamp,
		Level:     log.Level.String(),
		Labels:    make(map[string]string),
	}

	for k, v := range a.cfg.Labels {
		entry.Labels[k] = v
	}

	if log.Data.Fields != nil {
		for k, v := range log.Data.Fields {
			if strVal, ok := a.convertToString(v); ok {
				entry.Labels[k] = strVal
			}
		}
	}

	if log.Data.WithName && log.Data.Name != "" {
		entry.Labels["logger_name"] = log.Data.Name
	}

	entry.Labels["level"] = log.Level.String()

	var messageBuffer bytes.Buffer
	messageBuffer.WriteString(log.Message)

	if log.Data.Error != nil {
		if messageBuffer.Len() > 0 {
			messageBuffer.WriteString(" - ")
		}

		messageBuffer.WriteString("Error: ")
		messageBuffer.WriteString(log.Data.Error.Error())
	}

	if log.Data.Fields != nil {
		jsonFields := make(map[string]interface{})
		for k, v := range log.Data.Fields {
			if _, exists := entry.Labels[k]; !exists {
				jsonFields[k] = v
			}
		}

		if len(jsonFields) > 0 {
			if jsonData, err := json.Marshal(jsonFields); err == nil {
				if messageBuffer.Len() > 0 {
					messageBuffer.WriteString(" | ")
				}

				messageBuffer.Write(jsonData)
			}
		}
	}

	entry.Line = messageBuffer.String()

	select {
	case a.queue <- entry:
	default:
		fmt.Printf("Loki queue overflow, dropping log: %s\n", entry.Line)
	}
}

// Format formatting input log
func (a *lokiAdapter) Format(log *golog.Log) {
	if log.Data.Name != "" {
		log.Data.Name = fmt.Sprintf("[%s]: ", log.Data.Name)
		log.Message = log.Data.Name + log.Message
	}
}

// convertToString convert name to string
func (a *lokiAdapter) convertToString(v interface{}) (string, bool) {
	switch value := v.(type) {
	case string:
		return value, true
	case int, int8, int16, int32, int64:
		return fmt.Sprintf("%d", value), true
	case uint, uint8, uint16, uint32, uint64:
		return fmt.Sprintf("%d", value), true
	case float32, float64:
		return fmt.Sprintf("%f", value), true
	case bool:
		return fmt.Sprintf("%t", value), true
	default:
		return "", false
	}
}

// batchProcessor batch processor
func (a *lokiAdapter) batchProcessor() {
	defer a.wg.Done()

	ticker := time.NewTicker(a.cfg.BatchInterval)
	defer ticker.Stop()

	for {
		select {
		case entry := <-a.queue:
			a.bufferMutex.Lock()
			a.buffer = append(a.buffer, entry)
			a.bufferMutex.Unlock()

			if len(a.buffer) >= a.cfg.BatchSize {
				a.flush()
			}

		case <-ticker.C:
			a.bufferMutex.Lock()
			bufferLen := len(a.buffer)
			lastFlush := a.lastFlush
			a.bufferMutex.Unlock()

			if bufferLen > 0 && time.Since(lastFlush) >= a.cfg.BatchInterval {
				a.flush()
			}
		case <-a.quit:
			a.bufferMutex.Lock()
			bufferLen := len(a.buffer)
			a.bufferMutex.Unlock()
			if bufferLen > 0 {
				a.flush()
			}
			return
		}
	}
}

// flush sending all flushed logs in loki
func (a *lokiAdapter) flush() {
	if len(a.buffer) == 0 {
		return
	}

	a.bufferMutex.Lock()
	bufferCopy := make([]lokiLogEntry, len(a.buffer))
	copy(bufferCopy, a.buffer)
	a.buffer = a.buffer[:0]
	a.bufferMutex.Unlock()

	streams := make(map[string]lokiStream)

	for _, entry := range bufferCopy {
		labelsJSON, err := json.Marshal(entry.Labels)
		if err != nil {
			fmt.Printf("Error marshalling labels: %s\n", err.Error())
			continue
		}
		labelsKey := string(labelsJSON)

		stream, exists := streams[labelsKey]
		if !exists {
			stream = lokiStream{
				Stream: entry.Labels,
				Values: make([][2]string, 0),
			}
		}

		timestamp := entry.Timestamp.UnixNano()
		stream.Values = append(stream.Values, [2]string{
			fmt.Sprintf("%d", timestamp),
			entry.Line,
		})

		streams[labelsKey] = stream
	}

	payload := lokiPayload{
		Streams: make([]lokiStream, 0, len(streams)),
	}

	for _, stream := range streams {
		payload.Streams = append(payload.Streams, stream)
	}

	if ok := a.sendWithRetry(payload); ok {
		a.bufferMutex.Lock()
		a.lastFlush = time.Now()
		a.bufferMutex.Unlock()
	}
}

// sendWithRetry retry sending batch in Loki
func (a *lokiAdapter) sendWithRetry(payload lokiPayload) bool {
	for attempt := 0; attempt <= a.cfg.RetryCount; attempt++ {
		if attempt > 0 {
			time.Sleep(a.cfg.RetryDelay * time.Duration(attempt))
		}

		return a.send(payload)
	}

	fmt.Printf("Failed to send logs to Loki after %d attempts\n", a.cfg.RetryCount)
	return true
}

// send sending batch in Loki
func (a *lokiAdapter) send(payload lokiPayload) bool {
	jsonData, err := json.Marshal(payload)
	if err != nil {
		fmt.Printf("Error marshaling Loki payload: %v\n", err)
		return false
	}

	req, err := http.NewRequest("POST", a.cfg.Url, bytes.NewReader(jsonData))
	if err != nil {
		fmt.Printf("Error creating Loki request: %v\n", err)
		return false
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		fmt.Printf("Error sending to Loki: %v\n", err)
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return true
	}

	fmt.Printf("Loki returned error status: %d\n", resp.StatusCode)
	return false
}

// NewLokiAdapter returns new LokiAdapter
func NewLokiAdapter(cfg *LokiConfig) LokiAdapter {
	adapter := &lokiAdapter{
		cfg: cfg,

		client: &http.Client{
			Timeout: cfg.Timeout,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 100,
				IdleConnTimeout:     90 * time.Second,
			},
		},

		queue:  make(chan lokiLogEntry, cfg.BatchSize*10),
		quit:   make(chan struct{}),
		buffer: make([]lokiLogEntry, 0, cfg.BatchSize),

		lastFlush: time.Now(),
	}

	adapter.wg.Add(1)
	go adapter.batchProcessor()

	return adapter
}

// NewDefaultLokiAdapter returns new LokiAdapter with default config
func NewDefaultLokiAdapter(url string) LokiAdapter {
	cfg := defaultLokiConfig(url)

	adapter := &lokiAdapter{
		cfg: cfg,

		client: &http.Client{
			Timeout: cfg.Timeout,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 100,
				IdleConnTimeout:     90 * time.Second,
			},
		},

		queue:  make(chan lokiLogEntry, cfg.BatchSize*10),
		quit:   make(chan struct{}),
		buffer: make([]lokiLogEntry, 0, cfg.BatchSize),

		lastFlush: time.Now(),
	}

	adapter.wg.Add(1)
	go adapter.batchProcessor()

	return adapter
}
