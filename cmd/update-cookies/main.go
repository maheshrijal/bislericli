package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"bislericli/internal/config"
	"bislericli/internal/store"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

func main() {
	// Connect to existing Chrome on port 9222
	ctx, cancel := chromedp.NewRemoteAllocator(context.Background(), "http://localhost:9222")
	defer cancel()

	ctx, cancel = chromedp.NewContext(ctx)
	defer cancel()

	ctx, cancel = context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	fmt.Println("Extracting cookies from Chrome...")

	var cookies []*network.Cookie
	err := chromedp.Run(ctx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			var err error
			cookies, err = network.GetCookies().WithUrls([]string{
				"https://www.bisleri.com",
				"https://bisleri.com",
			}).Do(ctx)
			return err
		}),
	)

	if err != nil {
		log.Fatalf("Failed to get cookies: %v", err)
	}

	// Convert to store.Cookie format
	var storeCookies []store.Cookie
	for _, c := range cookies {
		storeCookies = append(storeCookies, store.Cookie{
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

	fmt.Printf("Captured %d cookies\n", len(storeCookies))

	// Load existing profile
	profilePath, err := config.ProfilePath("default")
	if err != nil {
		log.Fatalf("Failed to resolve profile path: %v", err)
	}
	
	profile, err := store.LoadProfile(profilePath)
	if err != nil {
		// Create new profile
		profile = store.Profile{Name: "default"}
	}

	// Update cookies and lastLogin
	profile.Cookies = storeCookies
	profile.LastLogin = time.Now()

	// Save profile
	if err := store.SaveProfile(profilePath, profile); err != nil {
		log.Fatalf("Failed to save profile: %v", err)
	}

	fmt.Println("✓ Profile updated successfully!")
	fmt.Printf("  Cookies: %d\n", len(profile.Cookies))
	fmt.Printf("  Profile: %s\n", profilePath)
}
