package distribution

import (
	"bytes"
	"github.com/docker/distribution/registry/client/transport"
	"golang.org/x/net/http/httpproxy"
	"io"
	"net/http"
	"os"
	"regexp"
)

var pushReg = regexp.MustCompile(`/v2/.*/blobs/uploads/.*`)

func NewMixTransport(t *http.Transport, modifier... transport.RequestModifier) http.RoundTripper {
	noproxyTr := t.Clone()
	noproxyTr.Proxy = nil

	return &MixTransport{
		transport.NewTransport(t, modifier...),
		transport.NewTransport(noproxyTr, modifier...),
	}
}

type MixTransport struct {
	proxyTransport   http.RoundTripper
	noProxyTransport http.RoundTripper
}

func HasProxy() bool {
	if proxyConfig := httpproxy.FromEnvironment(); proxyConfig.HTTPSProxy != "" || proxyConfig.HTTPProxy != "" {
		return true
	}

	return false
}

func (t *MixTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var hasProxy bool
	url, err := http.ProxyFromEnvironment(req)
	if err == nil && url != nil {
		hasProxy = true
	}
	var f *os.File
	var reader io.ReadCloser
	if hasProxy && req.Body != nil {
		if pushReg.MatchString(req.URL.Path) {
			f, err = os.CreateTemp("/tmp", "mixtransport*")
			defer func() {
				f.Close()
				os.Remove(f.Name())
			}()
			if err != nil {
				return nil, err
			}
			io.Copy(f, req.Body)
			req.Body.Close()
			f.Seek(0, io.SeekStart)
			req.Body = io.NopCloser(f)
		} else {
			b, _ := io.ReadAll(req.Body)
			req.Body.Close()
			req.Body = io.NopCloser(bytes.NewBuffer(b))
			reader = io.NopCloser(bytes.NewBuffer(b))
		}
	}
	resp, err := t.proxyTransport.RoundTrip(req)
	if hasProxy && (err != nil || resp.StatusCode > 399) {
		if f != nil {
			f.Seek(0, io.SeekStart)
			req.Body = io.NopCloser(f)
		} else if reader != nil {
			req.Body = reader
		}
		resp, err = t.noProxyTransport.RoundTrip(req)
	}

	return resp, err
}
