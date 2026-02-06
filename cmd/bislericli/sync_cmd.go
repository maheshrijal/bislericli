package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"bislericli/internal/bisleri"
	"bislericli/internal/config"
	"bislericli/internal/store"
)

func runSync(args []string) error {
	fs := flag.NewFlagSet("sync", flag.ContinueOnError)
	profileName := fs.String("profile", "", "Profile name (default: current/default)")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	cfg, err := config.LoadGlobalConfig()
	if err != nil {
		return err
	}

	name := resolveProfileName(*profileName, cfg)
	profile, _, err := loadOrCreateProfile(name)
	if err != nil {
		return err
	}

	if len(profile.Cookies) == 0 {
		return errors.New("no cookies in profile; run 'bislericli auth login'")
	}

	jar, err := bisleri.JarFromCookies(profile.Cookies)
	if err != nil {
		return err
	}

	client := bisleri.NewClient(&http.Client{Jar: jar, Timeout: 30 * time.Second}, log.New(os.Stderr, "bisleri: ", log.LstdFlags))
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	fmt.Printf("Syncing orders for profile '%s'...\n", name)

	// Fetch orders
	ordersHTML, resp, err := client.FetchPage(ctx, "/my-orders")
	if err != nil {
		return fmt.Errorf("failed to fetch orders: %w", err)
	}
	// Check auth
	if resp != nil && resp.Request != nil && resp.Request.URL != nil {
		if !strings.Contains(resp.Request.URL.Path, "/my-orders") {
			return errors.New("session expired; please run 'bislericli auth login'")
		}
	}

	parsedOrders, err := bisleri.ParseOrders(ordersHTML)
	if err != nil {
		return fmt.Errorf("failed to parse orders: %w", err)
	}

	fmt.Printf("Found %d orders on server.\n", len(parsedOrders))

	// Convert to store format
	var savedOrders []store.SavedOrder
	for _, o := range parsedOrders {
		amount, _ := bisleri.ParseINRAmount(o.Total)

		// Parse date for sorting/stats
		// Format seen: "05/01/2026, 11:49 AM"
		cleanedDate := strings.Split(o.Date, ",")[0] // Take part before comma "05/01/2026"
		cleanedDate = strings.TrimSpace(cleanedDate)

		t, err := time.Parse("02/01/2006", cleanedDate)
		if err != nil {
			// Try with time if split didn't work or different format
			formats := []string{
				"02/01/2006, 03:04 PM",
				"02/01/2006 03:04 PM",
				"January 02, 2006",
				"Jan 02, 2006",
			}
			for _, f := range formats {
				if parsed, err := time.Parse(f, o.Date); err == nil {
					t = parsed
					break
				}
			}
		}

		savedOrders = append(savedOrders, store.SavedOrder{
			OrderID:    o.OrderID,
			Date:       o.Date,
			ParsedDate: t,
			Status:     o.Status,
			Total:      o.Total,
			Amount:     amount,
			Items:      o.Items,
		})
	}

	if err := store.SaveOrderHistory(name, savedOrders); err != nil {
		return fmt.Errorf("failed to save history: %w", err)
	}

	fmt.Println("✓ Sync complete.")
	return nil
}
