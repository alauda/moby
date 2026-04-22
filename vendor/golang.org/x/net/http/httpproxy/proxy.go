package httpproxy

import "os"

// Config holds proxy settings loaded from environment variables.
type Config struct {
	HTTPProxy  string
	HTTPSProxy string
	NoProxy    string
	CGI        bool
}

// FromEnvironment returns proxy settings from the conventional environment variables.
func FromEnvironment() *Config {
	return &Config{
		HTTPProxy:  getEnvAny("HTTP_PROXY", "http_proxy"),
		HTTPSProxy: getEnvAny("HTTPS_PROXY", "https_proxy"),
		NoProxy:    getEnvAny("NO_PROXY", "no_proxy"),
		CGI:        os.Getenv("REQUEST_METHOD") != "",
	}
}

func getEnvAny(names ...string) string {
	for _, n := range names {
		if val := os.Getenv(n); val != "" {
			return val
		}
	}
	return ""
}
