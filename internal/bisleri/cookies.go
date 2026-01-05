package bisleri

import (
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"

	"bislericli/internal/store"
)

func JarFromCookies(cookies []store.Cookie) (*cookiejar.Jar, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	for _, c := range cookies {
		domain := strings.TrimPrefix(c.Domain, ".")
		if domain == "" {
			continue
		}
		u, err := url.Parse("https://" + domain)
		if err != nil {
			continue
		}
		cookie := &http.Cookie{
			Name:     c.Name,
			Value:    c.Value,
			Domain:   c.Domain,
			Path:     c.Path,
			Secure:   c.Secure,
			HttpOnly: c.HTTPOnly,
		}
		if c.Expires > 0 {
			cookie.Expires = time.Unix(c.Expires, 0)
		}
		jar.SetCookies(u, []*http.Cookie{cookie})
	}
	return jar, nil
}
