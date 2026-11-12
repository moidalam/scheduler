//go:build bvt

// Package bvt holds build-verification tests. They run against a live stack
// over real HTTP, with no mocks, and check each request three ways at once: the
// API result, the log lines it wrote (Loki), the metrics it moved (Prometheus)
// and its trace (Tempo). Run them with `make bvt`, which starts the stack first.
package bvt_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Each telemetry backend is reached on its own address, so a BVT can read what
// the service emitted for a request. Defaults match docker-compose.yml.
var (
	baseURL  = env("BVT_BASE_URL", "http://localhost:8080")
	lokiURL  = env("BVT_LOKI_URL", "http://localhost:3100")
	promURL  = env("BVT_PROMETHEUS_URL", "http://localhost:9090")
	tempoURL = env("BVT_TEMPO_URL", "http://localhost:3200")
)

// telemetryWait bounds how long a check waits for an export. The collector
// batches logs and traces for seconds and metrics every 10 seconds.
const telemetryWait = 60 * time.Second

func env(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

// requireStack fails the whole run early, with a useful message, when the
// service is not up. Without this every test would fail with a dial error.
func requireStack(t *testing.T) {
	t.Helper()
	resp, err := http.Get(baseURL + "/healthz")
	require.NoErrorf(t, err, "service not reachable at %s: run `make bvt`, or set BVT_BASE_URL", baseURL)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode, "healthz must be 200 before the BVTs run")
}

// apiResp is one response, with its body and headers kept for the telemetry checks.
type apiResp struct {
	Status int
	Body   []byte
	Header http.Header
}

func (r apiResp) decode(t *testing.T, v any) {
	t.Helper()
	require.NoError(t, json.Unmarshal(r.Body, v), "body: %s", r.Body)
}

// code returns the stable problem code, or "" for a successful response.
func (r apiResp) code(t *testing.T) string {
	t.Helper()
	var p struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(r.Body, &p)
	return p.Code
}

// api sends one request to the service. A tenant of "" sends no header.
func call(t *testing.T, method, path, tenant string, body any, headers map[string]string) apiResp {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		require.NoError(t, err)
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, baseURL+path, rdr)
	require.NoError(t, err)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if tenant != "" {
		req.Header.Set("X-Tenant-ID", tenant)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return apiResp{Status: resp.StatusCode, Body: b, Header: resp.Header}
}

// getJSON queries one telemetry backend and decodes the reply.
func getJSON(t *testing.T, endpoint string, params url.Values, v any) {
	t.Helper()
	resp, err := http.Get(endpoint + "?" + params.Encode())
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode, "%s", endpoint)
	require.NoError(t, json.NewDecoder(resp.Body).Decode(v))
}

// logLine is one record Loki stored for a request. Msg is the message, and
// Stream holds the labels and structured fields (level, op, error_code, ...).
type logLine struct {
	Msg    string
	Stream map[string]string
}

// logsForTrace returns every record with this trace id. It waits for the
// collector to export them, and fails if they do not arrive in time.
func logsForTrace(t *testing.T, traceID string) []logLine {
	t.Helper()
	var out []logLine
	require.Eventually(t, func() bool {
		out = nil
		now := time.Now()
		var body struct {
			Data struct {
				Result []struct {
					Stream map[string]string `json:"stream"`
					Values [][2]string       `json:"values"`
				} `json:"result"`
			} `json:"data"`
		}
		getJSON(t, lokiURL+"/loki/api/v1/query_range", url.Values{
			"query": {fmt.Sprintf(`{service_name="scheduler"} | trace_id = "%s"`, traceID)},
			"start": {strconv.FormatInt(now.Add(-time.Hour).UnixNano(), 10)},
			"end":   {strconv.FormatInt(now.Add(time.Minute).UnixNano(), 10)},
			"limit": {"200"},
		}, &body)
		for _, r := range body.Data.Result {
			for _, v := range r.Values {
				out = append(out, logLine{Msg: v[1], Stream: r.Stream})
			}
		}
		return len(out) > 0
	}, telemetryWait, 2*time.Second, "no log lines reached Loki for trace %s", traceID)
	return out
}

// findLog returns the one record with this message, failing if it is missing.
func findLog(t *testing.T, lines []logLine, msg string) logLine {
	t.Helper()
	for _, l := range lines {
		if l.Msg == msg {
			return l
		}
	}
	require.Failf(t, "missing log line", "no %q in %v", msg, lines)
	return logLine{}
}

// metricSum returns the current total of a metric, summed across its series
// that match the selector. Missing series count as zero.
func metricSum(t *testing.T, selector string) float64 {
	t.Helper()
	var body struct {
		Data struct {
			Result []struct {
				Value [2]any `json:"value"`
			} `json:"result"`
		} `json:"data"`
	}
	getJSON(t, promURL+"/api/v1/query", url.Values{"query": {"sum(" + selector + ")"}}, &body)
	if len(body.Data.Result) == 0 {
		return 0
	}
	s, _ := body.Data.Result[0].Value[1].(string)
	f, _ := strconv.ParseFloat(s, 64)
	return f
}

// waitForMetric waits until a metric series exists and is counting. It checks
// that the layer is wired to the collector, not the exact delta: counters are
// cumulative, so a restart resets them and a before/after comparison on a shared
// stack would be wrong.
func waitForMetric(t *testing.T, selector string) {
	t.Helper()
	require.Eventuallyf(t, func() bool {
		return metricSum(t, selector) > 0
	}, telemetryWait, 2*time.Second, "%s has no counted samples", selector)
}

// requireNoMetric asserts that a metric has never counted anything. It is used
// for error series, which must stay empty on a healthy stack.
func requireNoMetric(t *testing.T, selector string) {
	t.Helper()
	require.Zero(t, metricSum(t, selector), "%s counted something", selector)
}

// spansForTrace returns the names of every span Tempo holds for a trace.
func spansForTrace(t *testing.T, traceID string) []string {
	t.Helper()
	var names []string
	require.Eventually(t, func() bool {
		names = nil
		resp, err := http.Get(tempoURL + "/api/traces/" + traceID)
		if err != nil || resp.StatusCode != http.StatusOK {
			if resp != nil {
				resp.Body.Close()
			}
			return false
		}
		defer resp.Body.Close()
		var body struct {
			Batches []struct {
				ScopeSpans []struct {
					Spans []struct {
						Name string `json:"name"`
					} `json:"spans"`
				} `json:"scopeSpans"`
			} `json:"batches"`
		}
		if json.NewDecoder(resp.Body).Decode(&body) != nil {
			return false
		}
		for _, b := range body.Batches {
			for _, ss := range b.ScopeSpans {
				for _, sp := range ss.Spans {
					names = append(names, sp.Name)
				}
			}
		}
		return len(names) > 0
	}, telemetryWait, 2*time.Second, "no spans reached Tempo for trace %s", traceID)
	return names
}

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
