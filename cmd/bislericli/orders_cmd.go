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
)

func runOrders(args []string) error {
	fs := flag.NewFlagSet("orders", flag.ContinueOnError)
	profileName := fs.String("profile", "", "Profile name to use (default: current/default)")
	limit := fs.Int("limit", 10, "Maximum number of recent orders to display")
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
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	fmt.Println("Fetching order history...")

	// Fetch the my-orders page
	ordersHTML, resp, err := client.FetchPage(ctx, "/my-orders")
	if err != nil {
		return fmt.Errorf("failed to fetch orders: %w", err)
	}

	// Check if we got redirected (not logged in)
	if resp != nil && resp.Request != nil && resp.Request.URL != nil {
		if !strings.Contains(resp.Request.URL.Path, "/my-orders") {
			return errors.New("session expired; please run 'bislericli auth login'")
		}
	}

	// Parse orders
	orders, err := bisleri.ParseOrders(ordersHTML)
	if err != nil {
		return fmt.Errorf("failed to parse orders: %w", err)
	}

	if len(orders) == 0 {
		fmt.Println("No orders found.")
		return nil
	}

	// Limit the number of orders displayed
	if *limit > 0 && len(orders) > *limit {
		orders = orders[:*limit]
	}

	// Display orders in a nice table format
	fmt.Printf("\nOrder History (showing %d order(s)):\n\n", len(orders))
	fmt.Println(strings.Repeat("─", 80))
	fmt.Printf("%-20s  %-12s  %-20s  %-15s\n", "Order ID", "Date", "Status", "Total")
	fmt.Println(strings.Repeat("─", 80))

	for _, order := range orders {
		orderID := order.OrderID
		if len(orderID) > 20 {
			orderID = orderID[:17] + "..."
		}

		date := bisleri.FormatOrderDate(order.Date)
		if date == "" {
			date = order.Date
		}
		if len(date) > 12 {
			date = date[:9] + "..."
		}

		status := order.Status
		if len(status) > 20 {
			status = status[:17] + "..."
		}

		total := order.Total
		if len(total) > 15 {
			total = total[:12] + "..."
		}

		fmt.Printf("%-20s  %-12s  %-20s  %-15s\n", orderID, date, status, total)

		if order.Items != "" && len(order.Items) < 60 {
			fmt.Printf("  └─ %s\n", order.Items)
		}
	}

	fmt.Println(strings.Repeat("─", 80))
	fmt.Printf("\nMost recent order: %s\n", orders[0].OrderID)

	return nil
}
