package http_test

import (
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	hqgohttp "github.com/hueristiq/hq-lib-http-go"
)

var _ hqgohttp.Logger = (*slog.Logger)(nil)

type logRecord struct {
	level string
	msg   string
	args  map[string]any
}

type captureLogger struct {
	mu      sync.Mutex
	records []logRecord
}

func (l *captureLogger) add(level, msg string, args ...any) {
	fields := make(map[string]any, len(args)/2)
	for index := 0; index+1 < len(args); index += 2 {
		key, _ := args[index].(string)
		fields[key] = args[index+1]
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	l.records = append(l.records, logRecord{level: level, msg: msg, args: fields})
}

func (l *captureLogger) Debug(msg string, args ...any) { l.add("debug", msg, args...) }
func (l *captureLogger) Info(msg string, args ...any)  { l.add("info", msg, args...) }
func (l *captureLogger) Warn(msg string, args ...any)  { l.add("warn", msg, args...) }
func (l *captureLogger) Error(msg string, args ...any) { l.add("error", msg, args...) }

func (l *captureLogger) find(level, msg string) (record logRecord, ok bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	for _, record := range l.records {
		if record.level == level && record.msg == msg {
			return record, true
		}
	}

	return logRecord{}, false
}

func (l *captureLogger) empty() (empty bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	return len(l.records) == 0
}

func TestLoggerSuccess(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	logger := &captureLogger{}
	client := mustNew(t, hqgohttp.WithLogger(logger))
	response, err := client.Do(newRequest(t, http.MethodGet, server.URL, http.NoBody))
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())

	requestLog, ok := logger.find("debug", "http request")
	require.True(t, ok)
	assert.Equal(t, http.MethodGet, requestLog.args["method"])
	assert.Equal(t, 1, requestLog.args["attempt"])

	responseLog, ok := logger.find("debug", "http response")
	require.True(t, ok)
	assert.Equal(t, http.StatusOK, responseLog.args["status"])
	assert.Equal(t, 1, responseLog.args["attempt"])
}

func TestLoggerRetry(t *testing.T) {
	t.Parallel()

	var calls int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusInternalServerError)

			return
		}

		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	logger := &captureLogger{}
	client := mustNew(t, hqgohttp.WithRetryMaxAttempts(2), hqgohttp.WithLogger(logger))
	response, err := client.Do(newRequest(t, http.MethodGet, server.URL, http.NoBody))
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())

	retryLog, ok := logger.find("warn", "http retry")
	require.True(t, ok)
	assert.Equal(t, 1, retryLog.args["attempt"])
	assert.Equal(t, http.StatusInternalServerError, retryLog.args["status"])
}

func TestLoggerFailure(t *testing.T) {
	t.Parallel()

	logger := &captureLogger{}
	client := mustNew(t,
		hqgohttp.WithHTTPClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("transport failed")
		})}),
		hqgohttp.WithRetryMaxAttempts(1),
		hqgohttp.WithLogger(logger),
	)

	res, err := client.Do(newRequest(t, http.MethodGet, "http://example.com", http.NoBody)) //nolint:bodyclose // Transport failure returns no response.
	require.Error(t, err)
	assert.Nil(t, res)

	failureLog, ok := logger.find("error", "http request failed")
	require.True(t, ok)
	assert.Equal(t, 1, failureLog.args["attempts"])

	loggedErr, ok := failureLog.args["error"].(error)
	require.True(t, ok)
	assert.Error(t, loggedErr)
}

func TestLoggerRequestOverride(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	clientLogger := &captureLogger{}
	requestLogger := &captureLogger{}
	client := mustNew(t, hqgohttp.WithLogger(clientLogger))
	response, err := client.Do(
		newRequest(t, http.MethodGet, server.URL, http.NoBody),
		hqgohttp.WithRequestLogger(requestLogger),
	)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	assert.True(t, clientLogger.empty())
	assert.False(t, requestLogger.empty())
}

func TestLoggerRedactsURLPassword(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	requestURL, err := url.Parse(server.URL)
	require.NoError(t, err)

	requestURL.User = url.UserPassword("user", "secret")

	logger := &captureLogger{}
	client := mustNew(t, hqgohttp.WithLogger(logger))
	response, err := client.Do(newRequest(t, http.MethodGet, requestURL.String(), http.NoBody))
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())

	requestLog, ok := logger.find("debug", "http request")
	require.True(t, ok)
	assert.NotContains(t, requestLog.args["url"], "secret")
	assert.Contains(t, requestLog.args["url"], "xxxxx")
}

func TestLoggerDisabled(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	client := mustNew(t)
	response, err := client.Do(newRequest(t, http.MethodGet, server.URL, http.NoBody))
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
}
