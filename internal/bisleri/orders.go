package bisleri

import (
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// Order represents a Bisleri order
type Order struct {
	OrderID    string
	Date       string
	Status     string
	Total      string
	Items      string
	RawHTML    string // For debugging
}

// ParseOrders extracts order information from the my-orders HTML page
func ParseOrders(html string) ([]Order, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, err
	}

	var orders []Order



	// Find all order containers
	// Based on debug HTML, orders are wrapped in .all-order
	doc.Find(".all-order").Each(func(_ int, s *goquery.Selection) {
		order := Order{}
		
		orderText := s.Find(".order-section").Text()
		if match := orderTextRegex.FindStringSubmatch(orderText); len(match) > 0 {
			order.OrderID = match[0]
		}




		
		// Extract Date
		// Structure: Found <div class="order-date">...</div> or "Order Placed" block
		// Preference: .order-date seems most specific from grep
		order.Date = strings.TrimSpace(s.Find(".order-date").Text())
		if order.Date == "" {
			// Fallback: finding "Order Placed" label
			s.Find("div").EachWithBreak(func(_ int, div *goquery.Selection) bool {
				if strings.Contains(div.Text(), "Order Placed") {
					order.Date = strings.TrimSpace(div.Find("span").Text())
					return false
				}
				return true
			})
		}
		// Clean up date (remove timestamps if needed by caller, but keeping raw here is fine)
		// stats command parses "02/01/2006"

		// Extract Total
		s.Find(".row div").Each(func(_ int, col *goquery.Selection) {
			text := strings.TrimSpace(col.Text())
			if strings.Contains(strings.ToLower(text), "total price") {
				order.Total = strings.TrimSpace(col.Find("span").Text())
			}
		})

		// Extract Status
		// Structure: <div class="order-status-pending">Pending</div>
		// We try to find any element with class starting with order-status-
		s.Find("div").EachWithBreak(func(_ int, div *goquery.Selection) bool {
			class, _ := div.Attr("class")
			if strings.Contains(class, "order-status-") {
				order.Status = strings.TrimSpace(div.Text())
				return false
			}
			return true
		})

		// Items
		order.Items = strings.TrimSpace(s.Find(".one-time-order").Text())

		if order.OrderID != "" {
			orders = append(orders, order)
		}
	})
	
	return orders, nil
}

var orderTextRegex = regexp.MustCompile(`BS-[A-Z0-9-]+`)

// FormatOrderDate attempts to parse and format the order date
func FormatOrderDate(dateStr string) string {
	dateStr = strings.TrimSpace(dateStr)
	if dateStr == "" {
		return ""
	}

	// Try common formats
	formats := []string{
		"02/01/2006",
		"2006-01-02",
		"Jan 02, 2006",
		"02 Jan 2006",
		time.RFC3339,
	}

	for _, format := range formats {
		if t, err := time.Parse(format, dateStr); err == nil {
			return t.Format("02 Jan 2006")
		}
	}

	return dateStr // Return as-is if can't parse
}
