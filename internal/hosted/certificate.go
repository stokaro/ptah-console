package hosted

import (
	"crypto/tls"
	"log/slog"
	"os"
	"sync"
	"time"
)

// certificateFiles serves the certificate in two files, and reads them again
// when either one changes, which is how a Secret a certificate manager
// renews reaches a running Pod.
type certificateFiles struct {
	certFile, keyFile string
	logger            *slog.Logger

	mu     sync.Mutex
	loaded *tls.Certificate
	stamp  [2]time.Time
}

// get is tls.Config.GetCertificate. A pair that does not load, such as one
// caught half written, keeps the last good certificate in service.
func (c *certificateFiles) get(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	stamp, statErr := c.modified()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.loaded != nil && statErr == nil && stamp == c.stamp {
		return c.loaded, nil
	}
	pair, err := tls.LoadX509KeyPair(c.certFile, c.keyFile)
	if err != nil {
		if c.loaded != nil {
			if c.logger != nil {
				c.logger.Warn("the TLS certificate files did not load; serving the previous certificate", "error", err)
			}
			return c.loaded, nil
		}
		return nil, err
	}
	c.loaded, c.stamp = &pair, stamp
	return c.loaded, nil
}

func (c *certificateFiles) modified() ([2]time.Time, error) {
	var stamp [2]time.Time
	for i, name := range []string{c.certFile, c.keyFile} {
		info, err := os.Stat(name)
		if err != nil {
			return stamp, err
		}
		stamp[i] = info.ModTime()
	}
	return stamp, nil
}
