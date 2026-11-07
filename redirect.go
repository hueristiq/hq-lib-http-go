package http

import (
	"net/http"
	"strings"
)

// StripSensitiveHeadersOnRedirect returns a redirect policy that removes keys
// after a host change or an HTTPS-to-HTTP downgrade. It checks every hop so a
// later same-host redirect cannot restore a header stripped earlier.
func StripSensitiveHeadersOnRedirect(keys ...string) (checkRedirect func(req *http.Request, via []*http.Request) error) {
	return func(req *http.Request, via []*http.Request) (err error) {
		if len(via) == 0 || req.URL == nil {
			return nil
		}

		origin := via[0]
		strip := origin.URL == nil

		if !strip {
			for index, current := range via {
				if current.URL == nil || !strings.EqualFold(current.URL.Host, origin.URL.Host) {
					strip = true

					break
				}

				if index > 0 {
					previous := via[index-1]
					if previous.URL != nil && strings.EqualFold(previous.URL.Scheme, "https") && strings.EqualFold(current.URL.Scheme, "http") {
						strip = true

						break
					}
				}
			}
		}

		if !strip {
			last := via[len(via)-1]

			switch {
			case !strings.EqualFold(req.URL.Host, origin.URL.Host):
				strip = true
			case last.URL != nil && strings.EqualFold(last.URL.Scheme, "https") && strings.EqualFold(req.URL.Scheme, "http"):
				strip = true
			}
		}

		if !strip {
			return nil
		}

		for _, key := range keys {
			if key != "" {
				req.Header.Del(key)
			}
		}

		return nil
	}
}
