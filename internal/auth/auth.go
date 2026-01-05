package auth

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"
	"time"

	"bislericli/internal/store"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/storage"
	"github.com/chromedp/chromedp"
)

const bisleriHome = "https://www.bisleri.com/home"

func Login(ctx context.Context) ([]store.Cookie, error) {
	allocOpts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", false),
		chromedp.Flag("disable-gpu", false),
	)
	fmt.Println("Starting browser for Bisleri login...")
	allocCtx, cancel := chromedp.NewExecAllocator(ctx, allocOpts...)
	browserCtx, cancelBrowser := chromedp.NewContext(allocCtx)
	defer cancelBrowser()
	defer cancel()

	if err := chromedp.Run(browserCtx,
		network.Enable(),
		chromedp.Navigate(bisleriHome),
		chromedp.WaitReady("body", chromedp.ByQuery),
	); err != nil {
		return nil, err
	}

	fmt.Println("Browser opened. Please log in to Bisleri in the Chrome window.")
	fmt.Println("Waiting for login to complete automatically...")

	if err := waitForLogin(browserCtx, 5*time.Minute); err != nil {
		fmt.Println("Auto-login detection timed out. Press Enter to continue anyway.")
		reader := bufio.NewReader(os.Stdin)
		_, _ = reader.ReadString('\n')
	}

	filtered, err := captureCookies(browserCtx)
	if err != nil {
		return nil, err
	}

	if len(filtered) == 0 {
		return nil, errors.New("no Bisleri cookies captured; are you logged in?")
	}
	if err := verifyCookies(filtered); err != nil {
		return nil, err
	}

	// Let the deferred cancels close the browser context.
	time.Sleep(300 * time.Millisecond)
	return filtered, nil
}

type loginProbe struct {
	URL        string `json:"url"`
	Redirected bool   `json:"redirected"`
	Status     int    `json:"status"`
	HasLogout  bool   `json:"hasLogout"`
	HasAccount bool   `json:"hasAccount"`
}

func waitForLogin(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	attempt := 0
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if time.Now().After(deadline) {
			return errors.New("timed out waiting for login; try again")
		}
		attempt++
		ok, err := isLoggedIn(ctx, attempt)
		if err == nil && ok {
			return nil
		}
		// Backoff to reduce load and avoid traffic limits.
		delay := 500 * time.Millisecond
		switch {
		case attempt > 30:
			delay = 5 * time.Second
		case attempt > 15:
			delay = 3 * time.Second
		case attempt > 8:
			delay = 2 * time.Second
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
}

func isLoggedIn(ctx context.Context, attempt int) (bool, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	// Check cookies first
	netCookies, err := network.GetCookies().WithUrls([]string{
		bisleriHome,
		"https://www.bisleri.com",
		"https://bisleri.com",
	}).Do(probeCtx)
	if err == nil && hasLoginCookies(netCookies) {
		fmt.Println("✓ Login detected via cookies")
		return true, nil
	}

	if storageCookies, err := storage.GetCookies().Do(probeCtx); err == nil && hasLoginStorageCookies(storageCookies) {
		fmt.Println("✓ Login detected via storage cookies")
		return true, nil
	}

	// Check DOM more frequently (every 2 attempts instead of 4)
	if attempt%2 == 0 {
		var probe loginProbe
		err = chromedp.Run(probeCtx,
			chromedp.Evaluate(`(() => {
				try {
					const btn = document.querySelector('button[aria-label="Profile"], button[aria-haspopup="menu"], button[aria-expanded]');
					if (btn && !btn.getAttribute('data-bisleri-probe-clicked')) {
						btn.setAttribute('data-bisleri-probe-clicked', '1');
						btn.click();
					}
				} catch (e) {}
				const text = (document.body && document.body.innerText || '').toLowerCase();
				const hasLogout = text.includes('logout');
				const hasAccount = text.includes('my orders') || text.includes('account settings') || text.includes('manage addresses') || text.includes('bisleri wallet');
				return { url: location.href, redirected: false, status: 0, hasLogout, hasAccount };
			})()`, &probe),
		)
		if err == nil && (probe.HasLogout || probe.HasAccount) {
			fmt.Println("✓ Login detected via page content")
			return true, nil
		}
		if attempt%10 == 0 {
			// Debug output every 10 attempts
			fmt.Printf("  Still waiting... (attempt %d, hasLogout=%v, hasAccount=%v)\n", attempt, probe.HasLogout, probe.HasAccount)
		}
	}
	return false, nil
}

func hasLoginCookies(cookies []*network.Cookie) bool {
	for _, c := range cookies {
		if c == nil {
			continue
		}
		if isExpired(c.Expires) {
			continue
		}
		name := strings.ToLower(c.Name)
		if name == "sid" || name == "dwsid" || name == "dwuser" || name == "dwcustomer" {
			if c.Value != "" {
				return true
			}
		}
	}
	return false
}

func hasLoginStorageCookies(cookies []*network.Cookie) bool {
	for _, c := range cookies {
		if c == nil {
			continue
		}
		if isExpired(c.Expires) {
			continue
		}
		name := strings.ToLower(c.Name)
		if name == "sid" || name == "dwsid" || name == "dwuser" || name == "dwcustomer" {
			if c.Value != "" && strings.Contains(strings.ToLower(c.Domain), "bisleri.com") {
				return true
			}
		}
	}
	return false
}

func captureCookies(ctx context.Context) ([]store.Cookie, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var cookies []*network.Cookie
	if err := chromedp.Run(probeCtx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			var err error
			cookies, err = network.GetCookies().WithUrls([]string{
				bisleriHome,
				"https://www.bisleri.com",
				"https://bisleri.com",
			}).Do(ctx)
			return err
		}),
	); err == nil && len(cookies) > 0 {
		return filterNetworkCookies(cookies), nil
	}

	if storageCookies, err := storage.GetCookies().Do(probeCtx); err == nil {
		return filterStorageCookies(storageCookies), nil
	}
	return nil, errors.New("failed to read cookies")
}

func filterNetworkCookies(cookies []*network.Cookie) []store.Cookie {
	var filtered []store.Cookie
	for _, c := range cookies {
		if c == nil {
			continue
		}
		if isExpired(c.Expires) {
			continue
		}
		if !strings.Contains(c.Domain, "bisleri.com") {
			continue
		}
		filtered = append(filtered, store.Cookie{
			Name:     c.Name,
			Value:    c.Value,
			Domain:   c.Domain,
			Path:     c.Path,
			Expires:  int64(c.Expires),
			Secure:   c.Secure,
			HTTPOnly: c.HTTPOnly,
			SameSite: string(c.SameSite),
		})
	}
	return filtered
}

func filterStorageCookies(cookies []*network.Cookie) []store.Cookie {
	var filtered []store.Cookie
	for _, c := range cookies {
		if c == nil {
			continue
		}
		if isExpired(c.Expires) {
			continue
		}
		if !strings.Contains(strings.ToLower(c.Domain), "bisleri.com") {
			continue
		}
		filtered = append(filtered, store.Cookie{
			Name:     c.Name,
			Value:    c.Value,
			Domain:   c.Domain,
			Path:     c.Path,
			Expires:  int64(c.Expires),
			Secure:   c.Secure,
			HTTPOnly: c.HTTPOnly,
			SameSite: string(c.SameSite),
		})
	}
	return filtered
}

func isExpired(expires float64) bool {
	// Treat zero/negative as session cookies (not expired).
	if expires <= 0 {
		return false
	}
	now := float64(time.Now().Unix())
	return expires < now
}

func verifyCookies(cookies []store.Cookie) error {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return err
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

	client := &http.Client{Jar: jar, Timeout: 15 * time.Second}
	resp, err := client.Get("https://www.bisleri.com/my-orders")
	if err != nil {
		return fmt.Errorf("cookie verification failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.Request != nil && resp.Request.URL != nil {
		if !strings.Contains(resp.Request.URL.Path, "/my-orders") {
			return errors.New("cookies are not authenticated; please log in again")
		}
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("cookie verification failed: %s", resp.Status)
	}
	return nil
}

// Note: we avoid opening new tabs during login detection to keep UX seamless.
