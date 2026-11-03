package http

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ Logger = (*slog.Logger)(nil)

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

	for i := 0; i+1 < len(args); i += 2 {
		key, _ := args[i].(string)
		fields[key] = args[i+1]
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	l.records = append(l.records, logRecord{level: level, msg: msg, args: fields})
}

func (l *captureLogger) Debug(msg string, args ...any) { l.add("debug", msg, args...) }

func (l *captureLogger) Info(msg string, args ...any) { l.add("info", msg, args...) }

func (l *captureLogger) Warn(msg string, args ...any) { l.add("warn", msg, args...) }

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
	defer server.Close()

	logger := &captureLogger{}
	client := mustNew(t, WithLogger(logger))

	response, err := client.Get(server.URL)
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
	defer server.Close()

	logger := &captureLogger{}

	client := mustNew(t, WithRetryMaxAttempts(2), WithLogger(logger))

	response, err := client.Get(server.URL)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())

	retryLog, ok := logger.find("warn", "http retry")
	require.True(t, ok)
	assert.Equal(t, 1, retryLog.args["attempt"])
	assert.Equal(t, http.StatusInternalServerError, retryLog.args["status"])
}

func TestLoggerFailure(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	logger := &captureLogger{}

	client := mustNew(t, WithRetryMaxAttempts(1), WithLogger(logger))

	_, err := client.Get(server.URL)
	require.Error(t, err)

	failureLog, ok := logger.find("error", "http request failed")
	require.True(t, ok)
	assert.Equal(t, 1, failureLog.args["attempts"])
	assert.Error(t, failureLog.args["error"].(error)) //nolint:forcetypeassert // the logger always stores an error here.
}

func TestLoggerRequestOverride(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	clientLogger := &captureLogger{}
	requestLogger := &captureLogger{}

	client := mustNew(t, WithLogger(clientLogger))

	response, err := client.Get(server.URL, WithRequestLogger(requestLogger))
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
	defer server.Close()

	requestURL, err := url.Parse(server.URL)
	require.NoError(t, err)

	requestURL.User = url.UserPassword("user", "secret")

	logger := &captureLogger{}
	client := mustNew(t, WithLogger(logger))

	response, err := client.Get(requestURL.String())
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
	defer server.Close()

	client := mustNew(t)

	response, err := client.Get(server.URL)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
}
