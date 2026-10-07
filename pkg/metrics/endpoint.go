package metrics

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"

	"github.com/bitechdev/ResolveSpec/pkg/logger"
)

const textContentType = "text/plain; version=0.0.4; charset=utf-8"

// endpointPusher POSTs gathered metrics to a user-configured HTTP endpoint.
type endpointPusher struct {
	url       string
	format    string
	headers   map[string]string
	client    *http.Client
	resetOnOK bool
	provider  *PrometheusProvider
	gatherer  prometheus.Gatherer
	stopOnce  sync.Once
	stopCh    chan struct{}
	startedMu sync.Mutex
	started   bool
}

func newEndpointPusher(cfg *Config, p *PrometheusProvider) *endpointPusher {
	return &endpointPusher{
		url:       cfg.PushEndpointURL,
		format:    cfg.PushEndpointFormat,
		headers:   cfg.PushEndpointHeaders,
		client:    &http.Client{Timeout: time.Duration(cfg.PushEndpointTimeout) * time.Second},
		resetOnOK: cfg.PushEndpointResetOnSuccess,
		provider:  p,
		gatherer:  prometheus.DefaultGatherer,
		stopCh:    make(chan struct{}),
	}
}

func (e *endpointPusher) start(interval time.Duration) {
	e.startedMu.Lock()
	defer e.startedMu.Unlock()
	if e.started {
		return
	}
	e.started = true
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				if err := e.push(context.Background()); err != nil {
					logger.Warn("Failed to push metrics to endpoint %s: %v", e.url, err)
				}
			case <-e.stopCh:
				return
			}
		}
	}()
}

func (e *endpointPusher) stop() {
	e.stopOnce.Do(func() { close(e.stopCh) })
}

func (e *endpointPusher) push(ctx context.Context) error {
	mfs, err := e.gatherer.Gather()
	if err != nil && len(mfs) == 0 {
		return fmt.Errorf("gather metrics: %w", err)
	}

	body, contentType, err := encodeMetrics(mfs, e.format)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", contentType)
	for k, v := range e.headers {
		req.Header.Set(k, v)
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("endpoint returned %s: %s", resp.Status, bytes.TrimSpace(snippet))
	}

	if e.resetOnOK {
		e.provider.Reset()
	}
	return nil
}

func encodeMetrics(mfs []*dto.MetricFamily, format string) (body []byte, contentType string, err error) {
	switch format {
	case "json":
		b, err := json.Marshal(toJSONFamilies(mfs))
		return b, "application/json", err
	case "", "text":
		var buf bytes.Buffer
		enc := expfmt.NewEncoder(&buf, expfmt.NewFormat(expfmt.TypeTextPlain))
		for _, mf := range mfs {
			if err := enc.Encode(mf); err != nil {
				return nil, "", err
			}
		}
		return buf.Bytes(), textContentType, nil
	default:
		return nil, "", fmt.Errorf("unsupported push endpoint format %q", format)
	}
}

type jsonFamily struct {
	Name    string       `json:"name"`
	Help    string       `json:"help,omitempty"`
	Type    string       `json:"type"`
	Metrics []jsonMetric `json:"metrics"`
}

type jsonMetric struct {
	Labels  map[string]string `json:"labels,omitempty"`
	Value   *float64          `json:"value,omitempty"`
	Count   *uint64           `json:"count,omitempty"`
	Sum     *float64          `json:"sum,omitempty"`
	Buckets []jsonBucket      `json:"buckets,omitempty"`
}

type jsonBucket struct {
	UpperBound float64 `json:"le"`
	Count      uint64  `json:"count"`
}

func toJSONFamilies(mfs []*dto.MetricFamily) []jsonFamily {
	out := make([]jsonFamily, 0, len(mfs))
	for _, mf := range mfs {
		f := jsonFamily{Name: mf.GetName(), Help: mf.GetHelp(), Type: mf.GetType().String()}
		for _, m := range mf.GetMetric() {
			jm := jsonMetric{}
			if len(m.GetLabel()) > 0 {
				jm.Labels = make(map[string]string, len(m.GetLabel()))
				for _, l := range m.GetLabel() {
					jm.Labels[l.GetName()] = l.GetValue()
				}
			}
			switch {
			case m.Counter != nil:
				v := m.Counter.GetValue()
				jm.Value = &v
			case m.Gauge != nil:
				v := m.Gauge.GetValue()
				jm.Value = &v
			case m.Untyped != nil:
				v := m.Untyped.GetValue()
				jm.Value = &v
			case m.Histogram != nil:
				c, s := m.Histogram.GetSampleCount(), m.Histogram.GetSampleSum()
				jm.Count, jm.Sum = &c, &s
				for _, b := range m.Histogram.GetBucket() {
					jm.Buckets = append(jm.Buckets, jsonBucket{UpperBound: b.GetUpperBound(), Count: b.GetCumulativeCount()})
				}
			case m.Summary != nil:
				c, s := m.Summary.GetSampleCount(), m.Summary.GetSampleSum()
				jm.Count, jm.Sum = &c, &s
			}
			f.Metrics = append(f.Metrics, jm)
		}
		out = append(out, f)
	}
	return out
}
