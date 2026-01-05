package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"sort"
	"text/tabwriter"
	"time"

	"bislericli/internal/config"
	"bislericli/internal/store"
)

type monthStats struct {
	Yearmonth string // YYYY-MM
	MonthStr  string // "Jan 2026"
	Count     int
	Total     float64
}

func runStats(args []string) error {
	fs := flag.NewFlagSet("stats", flag.ContinueOnError)
	profileName := fs.String("profile", "", "Profile name to use (default: current/default)")
	viewPatterns := fs.Bool("view-patterns", false, "Analyze ordering patterns (day/time) instead of monthly history")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.LoadGlobalConfig()
	if err != nil {
		return err
	}

	name := resolveProfileName(*profileName, cfg)
	// We just need the name, but loadOrCreateProfile verifies it exists
	_, _, err = loadOrCreateProfile(name)
	if err != nil {
		return err
	}

	// Load local history
	history, err := store.LoadOrderHistory(name)
	if err != nil {
		if os.IsNotExist(err) {
			return errors.New("no synced data found; run 'bisleri sync' first")
		}
		return fmt.Errorf("failed to load history: %w", err)
	}

	orders := history.Orders
	if len(orders) == 0 {
		fmt.Println("No orders found in local history.")
		return nil
	}

	fmt.Printf("Analyzing %d orders (last synced: %s)\n", len(orders), history.LastSynced.Format("2006-01-02 15:04"))

	if *viewPatterns {
		printPatterns(orders)
	} else {
		printMonthlyStats(orders)
	}

	return nil
}

func printMonthlyStats(orders []store.SavedOrder) {
	statsMap := make(map[string]*monthStats)
	var earliest, latest string
	var totalOrders int
	var grandTotal float64

	for _, o := range orders {
		// Skip invalid orders
		if o.Amount == 0 && o.Total != "0" && o.Total != "Free" {
             // Maybe try fix? Already fixed in sync
		}
		
		t := o.ParsedDate
		if t.IsZero() {
			continue
		}

		ym := t.Format("2006-01")
		if _, exists := statsMap[ym]; !exists {
			statsMap[ym] = &monthStats{
				Yearmonth: ym,
				MonthStr:  t.Format("Jan 2006"),
			}
		}
		statsMap[ym].Count++
		statsMap[ym].Total += o.Amount

		grandTotal += o.Amount
		totalOrders++
		
		dStr := t.Format("2006-01-02")
		if earliest == "" || dStr < earliest {
			earliest = dStr
		}
		if latest == "" || dStr > latest {
			latest = dStr
		}
	}


	// Sort keys
	var keys []string
	for k := range statsMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// Print Table
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Println()
	fmt.Fprintln(w, "+----------------+----------+---------------+---------------+")
	fmt.Fprintln(w, "| Period\t| Orders\t| Total\t| Average\t|")
	fmt.Fprintln(w, "+----------------+----------+---------------+---------------+")

	for _, k := range keys {
		s := statsMap[k]
		avg := 0.0
		if s.Count > 0 {
			avg = s.Total / float64(s.Count)
		}
		fmt.Fprintf(w, "| %s\t| %d\t| ₹%.2f\t| ₹%.2f\t|\n", s.MonthStr, s.Count, s.Total, avg)
	}
	fmt.Fprintln(w, "+----------------+----------+---------------+---------------+")
	w.Flush()

	// Print Footer
	fmt.Println()
	fmt.Fprintln(w, "+----------+---------------+---------------+---------------+---------------+")
	fmt.Fprintln(w, "| Orders\t| Total\t| Average\t| Earliest\t| Latest\t|")
	fmt.Fprintln(w, "+----------+---------------+---------------+---------------+---------------+")
	
	grandAvg := 0.0
	if totalOrders > 0 {
		grandAvg = grandTotal / float64(totalOrders)
	}
	
	fmt.Fprintf(w, "| %d\t| ₹%.2f\t| ₹%.2f\t| %s\t| %s\t|\n", totalOrders, grandTotal, grandAvg, earliest, latest)
	fmt.Fprintln(w, "+----------+---------------+---------------+---------------+---------------+")
	w.Flush()
	fmt.Println()
}

func printPatterns(orders []store.SavedOrder) {
	// Day of Week Stats
	dowMap := make(map[time.Weekday]int)
	totalOrders := 0

	for _, o := range orders {
		t := o.ParsedDate
		if t.IsZero() {
			continue
		}
		dowMap[t.Weekday()]++
		totalOrders++
	}

	if totalOrders == 0 {
		fmt.Println("No valid dates found for pattern analysis.")
		return
	}

	fmt.Println("Ordering patterns")
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "+----------------+----------+----------+")
	fmt.Fprintln(w, "| Day\t| Orders\t| Share\t|")
	fmt.Fprintln(w, "+----------------+----------+----------+")

	// Order from Monday to Sunday
	weekdays := []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday, time.Saturday, time.Sunday}
	
	for _, d := range weekdays {
		count := dowMap[d]
		share := 0.0
		if totalOrders > 0 {
			share = (float64(count) / float64(totalOrders)) * 100
		}
		// Color logic could be added here if ANSI allowed (User requested pretty UI)
		// but standard go fmt is safer.
		fmt.Fprintf(w, "| %s\t| %d\t| %.1f%%\t|\n", d.String(), count, share)
	}
	fmt.Fprintln(w, "+----------------+----------+----------+")
	w.Flush()
}
