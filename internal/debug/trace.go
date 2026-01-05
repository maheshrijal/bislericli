package debug

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"
	"time"

	"bislericli/internal/store"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

func RunOrderDebug(ctx context.Context, profile store.Profile) error {
	// Setup chrome options for visible window
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", false),
		chromedp.Flag("disable-gpu", false),
		chromedp.Flag("enable-automation", false),
	)

	allocCtx, cancel := chromedp.NewExecAllocator(ctx, opts...)
	defer cancel()

	// create context
	ctx, cancel = chromedp.NewContext(allocCtx)
	defer cancel()

	if err := chromedp.Run(ctx, network.Enable()); err != nil {
		return err
	}

	fmt.Println("Opening visible Chrome window...")
	
	// Set cookies if we have them
	if len(profile.Cookies) > 0 {
		fmt.Println("Restoring session cookies...")
		if err := setCookies(ctx, profile.Cookies); err != nil {
			fmt.Printf("Warning: failed to set cookies: %v\n", err)
		}
	}

	// Navigate to home and check login status
	fmt.Println("Navigating to home page to check login status...")
	var currentURL string
	if err := chromedp.Run(ctx,
		chromedp.Navigate("https://www.bisleri.com/home"),
		chromedp.WaitVisible("body", chromedp.ByQuery),
		chromedp.Location(&currentURL),
	); err != nil {
		return err
	}

	// Interactive login verification
	fmt.Println("Please verify in the Chrome window: Are you logged in?")
	fmt.Println("If not, please LOG IN MANUALLY now.")
	fmt.Println("Press ENTER in this terminal once you are fully logged in and ready to proceed...")
	reader := bufio.NewReader(os.Stdin)
	_, _ = reader.ReadString('\n')

	// Refresh cookies from browser to ensure we have the latest session
	fmt.Println("Capturing latest session cookies...")
	_, err := network.GetCookies().Do(ctx)
	if err != nil {
		return fmt.Errorf("failed to capture cookies: %w", err)
	}
	// We could save these back to profile if we wanted, but for debug we just use them in-session

	fmt.Println("Navigating to cart and listening for checkout requests...")

	// Listen for network events
	chromedp.ListenTarget(ctx, func(ev interface{}) {
		switch e := ev.(type) {
		case *network.EventRequestWillBeSent:
			if strings.EqualFold(e.Request.Method, "POST") || strings.Contains(e.Request.URL, "checkout") {
				fmt.Printf("\n[Request] %s %s\n", e.Request.Method, e.Request.URL)
				fmt.Printf("  Type: %s\n", e.Type)
				if len(e.Request.Headers) > 0 {
					fmt.Println("  Headers:")
					for k, v := range e.Request.Headers {
						val := fmt.Sprint(v)
						if strings.EqualFold(k, "Cookie") || strings.EqualFold(k, "Authorization") {
							val = "[REDACTED]"
						}
						fmt.Printf("    %s: %s\n", k, val)
					}
				}
				if e.Request.HasPostData {
					// Using println to avoid build errors with undefined fields in some versions
					fmt.Println("  PostData: (present)")
					// fmt.Printf("  PostData: %s\n", e.Request.PostData) 
				}
			}
		case *network.EventResponseReceived:
			if strings.Contains(e.Response.URL, "checkout") || e.Response.Status >= 400 {
				fmt.Printf("\n[Response] (%d) %s\n", e.Response.Status, e.Response.URL)
				fmt.Printf("  MimeType: %s\n", e.Response.MimeType)
			}
		}
	})

	return chromedp.Run(ctx,
		chromedp.Navigate("https://www.bisleri.com/mycart"),
		chromedp.WaitVisible("body", chromedp.ByQuery),
		chromedp.Sleep(2*time.Second), // Wait for cart to settle
		chromedp.ActionFunc(func(ctx context.Context) error {
			fmt.Println("Searching for Checkout button...")
			return nil
		}),
		// Attempt to click the checkout button
		chromedp.Click(`//a[contains(text(), "Checkout")] | //button[contains(text(), "Checkout")]`, chromedp.BySearch),
		chromedp.ActionFunc(func(ctx context.Context) error {
			fmt.Println("Clicked Checkout. Waiting for network activity...")
			return nil
		}),
		chromedp.Sleep(15*time.Second), // Wait long enough to capture the request
	)
}

func setCookies(ctx context.Context, cookies []store.Cookie) error {
	return chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		for _, c := range cookies {
			builder := network.SetCookie(c.Name, c.Value).
				WithDomain(c.Domain).
				WithPath(c.Path).
				WithSecure(c.Secure).
				WithHTTPOnly(c.HTTPOnly)

			// Bypass explicit expiration setting to avoid type issues and treat as session cookies
			// if c.Expires != 0 { ... }
			
			if err := builder.Do(ctx); err != nil {
				return err
			}
		}
		return nil
	}))
}

// Helper to convert cookies (duplicated logic, should ideally be shared but keeping isolated for debug)
func cookieJar(cookies []store.Cookie) *http.Client {
	jar, _ := cookiejar.New(nil)
	for _, c := range cookies {
		u, _ := url.Parse("https://" + strings.TrimPrefix(c.Domain, "."))
		httpC := &http.Cookie{
			Name:   c.Name,
			Value:  c.Value,
			Domain: c.Domain,
			Path:   c.Path,
		}
		jar.SetCookies(u, []*http.Cookie{httpC})
	}
	return &http.Client{Jar: jar}
}
