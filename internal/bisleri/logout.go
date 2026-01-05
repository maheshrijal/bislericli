package bisleri

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

var logoutFallbackPaths = []string{
	"/on/demandware.store/Sites-Bis-Site/default/Account-Logout",
	"/on/demandware.store/Sites-Bis-Site/default/Login-Logout",
	"/on/demandware.store/Sites-Bis-Site/default/Logout-Logout",
	"/logout",
}

func (c *Client) Logout(ctx context.Context) error {
	logoutURL := ""
	if html, _, err := c.fetchPage(ctx, "/my-orders"); err == nil {
		logoutURL = extractLogoutURL(html)
	}
	if logoutURL != "" {
		if err := c.hitLogout(ctx, logoutURL); err == nil {
			return nil
		}
	}

	for _, path := range logoutFallbackPaths {
		if err := c.hitLogout(ctx, c.newURL(path)); err == nil {
			return nil
		}
	}
	return errors.New("logout failed")
}

func (c *Client) hitLogout(ctx context.Context, rawURL string) error {
	logoutURL := rawURL
	if strings.HasPrefix(rawURL, "/") {
		logoutURL = c.newURL(rawURL)
	}
	parsed, err := url.Parse(logoutURL)
	if err != nil {
		return err
	}
	req, err := http.NewRequest("GET", parsed.String(), nil)
	if err != nil {
		return err
	}
	resp, err := c.do(ctx, req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		return nil
	}
	return errors.New("logout request failed")
}

func extractLogoutURL(html string) string {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return ""
	}
	var logoutURL string
	doc.Find("a").EachWithBreak(func(_ int, s *goquery.Selection) bool {
		text := strings.ToLower(strings.TrimSpace(s.Text()))
		href, _ := s.Attr("href")
		href = strings.TrimSpace(href)
		if href == "" {
			return true
		}
		if strings.Contains(text, "logout") || strings.Contains(strings.ToLower(href), "logout") {
			logoutURL = href
			return false
		}
		return true
	})
	return logoutURL
}
