package distribution

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"regexp"

	"github.com/docker/distribution/registry/client/transport"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"golang.org/x/net/http/httpproxy"
)

var pushReg = regexp.MustCompile(`/v2/.*/blobs/uploads/.*`)

// newTransport creates a new transport which will apply modifiers to
// the request on a RoundTrip call.
func newTransport(base http.RoundTripper, modifiers ...transport.RequestModifier) http.RoundTripper {
	var tr http.RoundTripper
	if baseTransport, ok := base.(*http.Transport); ok && hasProxy() {
		tr = newMixTransport(baseTransport, modifiers...)
	} else {
		tr = transport.NewTransport(base, modifiers...)
	}

	// Wrap the transport with OpenTelemetry instrumentation
	// This propagates the Traceparent header.
	return otelhttp.NewTransport(tr)
}

func newMixTransport(t *http.Transport, modifiers ...transport.RequestModifier) http.RoundTripper {
	noproxyTr := t.Clone()
	noproxyTr.Proxy = nil

	return &mixTransport{
		proxyTransport:   transport.NewTransport(t, modifiers...),
		noProxyTransport: transport.NewTransport(noproxyTr, modifiers...),
	}
}

type mixTransport struct {
	proxyTransport   http.RoundTripper
	noProxyTransport http.RoundTripper
}

func hasProxy() bool {
	proxyConfig := httpproxy.FromEnvironment()
	return proxyConfig.HTTPSProxy != "" || proxyConfig.HTTPProxy != ""
}

func (t *mixTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var useProxy bool
	if proxyURL, err := http.ProxyFromEnvironment(req); err == nil && proxyURL != nil {
		useProxy = true
	}

	var (
		f      *os.File
		reader io.ReadCloser
	)
	if useProxy && req.Body != nil {
		if pushReg.MatchString(req.URL.Path) {
			var err error
			f, err = os.CreateTemp("/tmp", "mixtransport*")
			if err != nil {
				return nil, err
			}
			defer func() {
				f.Close()
				os.Remove(f.Name())
			}()
			if _, err := io.Copy(f, req.Body); err != nil {
				req.Body.Close()
				return nil, err
			}
			req.Body.Close()
			if _, err := f.Seek(0, io.SeekStart); err != nil {
				return nil, err
			}
			req.Body = io.NopCloser(f)
		} else {
			b, err := io.ReadAll(req.Body)
			req.Body.Close()
			if err != nil {
				return nil, err
			}
			req.Body = io.NopCloser(bytes.NewBuffer(b))
			reader = io.NopCloser(bytes.NewBuffer(b))
		}
	}

	resp, err := t.proxyTransport.RoundTrip(req)
	if useProxy && (err != nil || resp.StatusCode > 399) {
		if f != nil {
			if _, seekErr := f.Seek(0, io.SeekStart); seekErr != nil {
				return nil, seekErr
			}
			req.Body = io.NopCloser(f)
		} else if reader != nil {
			req.Body = reader
		}
		resp, err = t.noProxyTransport.RoundTrip(req)
	}

	return resp, err
}
