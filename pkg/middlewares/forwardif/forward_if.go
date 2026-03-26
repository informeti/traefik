package forwardif

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/traefik/traefik/v3/pkg/config/dynamic"
	"github.com/traefik/traefik/v3/pkg/middlewares"
)

const (
	typeName = "ForwardIf"
)

// Middleware used to conditionally forward a URL request to another endpoint and return its endpoint without processing other further middlewares.
// TODO: support `host` (to avoid repeating the scheme)
// TODO: support a comparison operator
// TODO: support comparing query parameters, cookies, ...
type forwardIf struct {
	next        http.Handler
	endpoint    string
	headerName  string
	headerValue string
	timeout     time.Duration
	name        string
	client      *http.Client
}

func New(ctx context.Context, next http.Handler, config dynamic.ForwardIf, name string) (http.Handler, error) {
	middlewares.GetLogger(ctx, name, typeName).Debug().Msg("Creating middleware")

	if config.Endpoint == "" {
		return nil, errors.New("endpoint must be set")
	}

	if config.HeaderName == "" {
		return nil, errors.New("headerName must be set")
	}

	if config.HeaderValue == "" {
		return nil, errors.New("headerValue must be set")
	}

	timeout := time.Duration(config.Timeout)
	if timeout == 0 {
		timeout = 12 * time.Second
	}

	result := &forwardIf{
		endpoint:    config.Endpoint,
		headerName:  config.HeaderName,
		headerValue: config.HeaderValue,
		timeout:     timeout,
		next:        next,
		name:        name,
		client: &http.Client{
			Timeout: timeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}

	return result, nil
}

func (r *forwardIf) GetTracingInformation() (string, string) {
	return r.name, typeName
}

func (r *forwardIf) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	logger := middlewares.GetLogger(req.Context(), r.name, typeName)

	// Check if the header matches the expected value
	/*headerValue := req.Header.Get(r.headerName)
	if headerValue != r.headerValue {
		logger.Debug().
			Str("headerName", r.headerName).
			Str("expectedValue", r.headerValue).
			Str("actualValue", headerValue).
			Msg("Header does not match, passing to next handler")
		r.next.ServeHTTP(rw, req)
		return
	}*/

	if !shouldForward(req, r.headerName, r.headerValue) {
		logger.Debug().
			Str("headerName", r.headerName).
			Str("expectedValue", r.headerValue).
			Str("actualValue", req.Header.Get(r.headerName)).
			Msg("Header does not match, passing to next handler")
		r.next.ServeHTTP(rw, req)
		return
	}

	// Read the entire request body
	var bodyBytes []byte
	var err error
	if req.Body != nil {
		bodyBytes, err = io.ReadAll(req.Body)
		if err != nil {
			logger.Error().Err(err).Msg("Failed to read request body")
			http.Error(rw, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		req.Body.Close()
	}

	forwardURL := getForwardURL(r.endpoint, req)
	logger.Debug().Msg("Forwarding request to: " + forwardURL)

	forwardReq, err := http.NewRequestWithContext(req.Context(), req.Method, forwardURL, bytes.NewReader(bodyBytes))
	if err != nil {
		logger.Error().Err(err).Msg("Failed to create forwarded request")
		http.Error(rw, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	addRequestHeaders(forwardReq, req.Header)

	resp, err := r.client.Do(forwardReq)
	if err != nil {
		logger.Error().Err(err).Msg("Failed to forward request to endpoint")
		http.Error(rw, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	addResponseHeaders(rw, resp.Header)

	rw.WriteHeader(resp.StatusCode)

	// Copy response body
	_, err = io.Copy(rw, resp.Body)
	if err != nil {
		logger.Error().Err(err).Msg("Failed to copy response body")
		return
	}

	logger.Debug().
		Int("statusCode", resp.StatusCode).
		Msg("Successfully forwarded request and returned response")
}

func shouldForward(req *http.Request, headerName, headerValue string) bool {
	return req.Header.Get(headerName) == headerValue
}

func getForwardURL(endpoint string, req *http.Request) string {
	forwardURL := endpoint + req.URL.Path
	if req.URL.RawQuery != "" {
		forwardURL += "?" + req.URL.RawQuery
	}
	return forwardURL
}

func addRequestHeaders(req *http.Request, headers http.Header) {
	for name, values := range headers {
		for _, value := range values {
			req.Header.Add(name, value)
		}
	}
}

func addResponseHeaders(rw http.ResponseWriter, headers http.Header) {
	for name, values := range headers {
		for _, value := range values {
			rw.Header().Add(name, value)
		}
	}
}
