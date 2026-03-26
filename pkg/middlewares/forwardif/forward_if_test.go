package forwardif

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ptypes "github.com/traefik/paerser/types"
	"github.com/traefik/traefik/v3/pkg/config/dynamic"
)

func TestNew(t *testing.T) {
	testCases := []struct {
		desc         string
		config       dynamic.ForwardIf
		expectsError bool
		errorMsg     string
	}{
		{
			desc: "Valid configuration",
			config: dynamic.ForwardIf{
				Endpoint:    "http://example.com",
				HeaderName:  "X-Test",
				HeaderValue: "test-value",
			},
			expectsError: false,
		},
		{
			desc: "Missing endpoint",
			config: dynamic.ForwardIf{
				HeaderName:  "X-Test",
				HeaderValue: "test-value",
			},
			expectsError: true,
			errorMsg:     "endpoint must be set",
		},
		{
			desc: "Missing header name",
			config: dynamic.ForwardIf{
				Endpoint:    "http://example.com",
				HeaderValue: "test-value",
			},
			expectsError: true,
			errorMsg:     "headerName must be set",
		},
		{
			desc: "Missing header value",
			config: dynamic.ForwardIf{
				Endpoint:   "http://example.com",
				HeaderName: "X-Test",
			},
			expectsError: true,
			errorMsg:     "headerValue must be set",
		},
	}

	for _, test := range testCases {
		t.Run(test.desc, func(t *testing.T) {
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

			handler, err := New(context.Background(), next, test.config, "test-forward-if")
			if test.expectsError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), test.errorMsg)
				assert.Nil(t, handler)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, handler)
			}
		})
	}
}

func TestForwardIf_HeaderNotMatch(t *testing.T) {
	// Setup next handler that should be called when header doesn't match
	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("next handler response"))
	})

	config := dynamic.ForwardIf{
		Endpoint:    "http://example.com",
		HeaderName:  "X-Forward",
		HeaderValue: "forward-me",
	}

	handler, err := New(context.Background(), next, config, "test-forward-if")
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "http://localhost/test", nil)
	req.Header.Set("X-Forward", "dont-forward-me")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	assert.True(t, nextCalled)
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "next handler response", recorder.Body.String())
}

func TestForwardIf_HeaderMatch(t *testing.T) {
	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/test/path", r.URL.Path)
		assert.Equal(t, "query=value", r.URL.RawQuery)
		assert.Equal(t, "forward-me", r.Header.Get("X-Forward"))
		assert.Equal(t, "custom-value", r.Header.Get("X-Custom"))

		w.Header().Set("X-Response-Header", "response-value")
		w.WriteHeader(http.StatusAccepted)
		w.Write([]byte("forwarded response"))
	}))
	defer targetServer.Close()

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
	})

	config := dynamic.ForwardIf{
		Endpoint:    targetServer.URL,
		HeaderName:  "X-Forward",
		HeaderValue: "forward-me",
	}

	handler, err := New(context.Background(), next, config, "test-forward-if")
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "http://localhost/test/path?query=value", nil)
	req.Header.Set("X-Forward", "forward-me")
	req.Header.Set("X-Custom", "custom-value")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	assert.False(t, nextCalled)
	assert.Equal(t, http.StatusAccepted, recorder.Code)
	assert.Equal(t, "forwarded response", recorder.Body.String())
	assert.Equal(t, "response-value", recorder.Header().Get("X-Response-Header"))
}

func TestForwardIf_HeaderMatch_CaseInsensitive(t *testing.T) {
	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/test/path", r.URL.Path)
		assert.Equal(t, "query=value", r.URL.RawQuery)
		assert.Equal(t, "forward-me", r.Header.Get("X-Forward"))
		assert.Equal(t, "custom-value", r.Header.Get("X-Custom"))

		w.Header().Set("X-Response-Header", "response-value")
		w.WriteHeader(http.StatusAccepted)
		w.Write([]byte("forwarded response"))
	}))
	defer targetServer.Close()

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
	})

	config := dynamic.ForwardIf{
		Endpoint:    targetServer.URL,
		HeaderName:  "X-Forward",
		HeaderValue: "forward-me",
	}

	handler, err := New(context.Background(), next, config, "test-forward-if")
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "http://localhost/test/path?query=value", nil)
	req.Header.Set("x-forward", "forward-me")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	assert.False(t, nextCalled)
	assert.Equal(t, http.StatusAccepted, recorder.Code)
	assert.Equal(t, "forwarded response", recorder.Body.String())
	assert.Equal(t, "response-value", recorder.Header().Get("X-Response-Header"))
}

func TestForwardIf_WithRequestBody(t *testing.T) {
	requestBody := "test request body"
	receivedBody := ""

	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		receivedBody = string(body)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("response"))
	}))
	defer targetServer.Close()

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

	config := dynamic.ForwardIf{
		Endpoint:    targetServer.URL,
		HeaderName:  "X-Forward",
		HeaderValue: "yes",
	}

	handler, err := New(context.Background(), next, config, "test-forward-if")
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "http://localhost/test", strings.NewReader(requestBody))
	req.Header.Set("X-Forward", "yes")
	req.Header.Set("Content-Type", "text/plain")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, requestBody, receivedBody)
}

func TestForwardIf_DifferentHTTPMethods(t *testing.T) {
	methods := []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch}

	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			receivedMethod := ""

			targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				receivedMethod = r.Method
				w.WriteHeader(http.StatusOK)
			}))
			defer targetServer.Close()

			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

			config := dynamic.ForwardIf{
				Endpoint:    targetServer.URL,
				HeaderName:  "X-Forward",
				HeaderValue: "yes",
			}

			handler, err := New(context.Background(), next, config, "test-forward-if")
			require.NoError(t, err)

			req := httptest.NewRequest(method, "http://localhost/test", nil)
			req.Header.Set("X-Forward", "yes")
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, req)

			assert.Equal(t, method, receivedMethod)
			assert.Equal(t, http.StatusOK, recorder.Code)
		})
	}
}

func TestForwardIf_Timeout(t *testing.T) {
	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Make the server exceed the timeout
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer targetServer.Close()

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

	config := dynamic.ForwardIf{
		Endpoint:    targetServer.URL,
		HeaderName:  "X-Forward",
		HeaderValue: "yes",
		Timeout:     ptypes.Duration(50 * time.Millisecond),
	}

	handler, err := New(context.Background(), next, config, "test-forward-if")
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "http://localhost/test", nil)
	req.Header.Set("X-Forward", "yes")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusInternalServerError, recorder.Code)
}

func TestForwardIf_UnreachableEndpoint(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

	config := dynamic.ForwardIf{
		Endpoint:    "http://localhost:65536",
		HeaderName:  "X-Forward",
		HeaderValue: "yes",
	}

	handler, err := New(context.Background(), next, config, "test-forward-if")
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "http://localhost/test", nil)
	req.Header.Set("X-Forward", "yes")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusInternalServerError, recorder.Code)
}

func TestForwardIf_DefaultTimeout(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

	config := dynamic.ForwardIf{
		Endpoint:    "http://example.com",
		HeaderName:  "X-Forward",
		HeaderValue: "yes",
	}

	handler, err := New(context.Background(), next, config, "test-forward-if")
	require.NoError(t, err)

	forwardIfHandler, ok := handler.(*forwardIf)
	require.True(t, ok)
	assert.Equal(t, 12*time.Second, forwardIfHandler.timeout)
}
