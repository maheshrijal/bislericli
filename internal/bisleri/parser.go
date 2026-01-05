package bisleri

import (
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"bislericli/internal/store"

	"github.com/PuerkitoBio/goquery"
)

type AddressCandidate struct {
	ID        string
	Address   store.Address
	IsDefault bool
	RawText   string
}

type CheckoutForm struct {
	Action string
	Method string
	Fields url.Values
}

type CheckoutCandidate struct {
	Action string
	Method string
	Source string
}

var (
	csrfRegex         = regexp.MustCompile(`name=["']csrf_token["']\s+value=["']([^"']+)["']`)
	shipmentUUIDRegex = regexp.MustCompile(`shipmentUUID[^"'\w]*["']?([a-f0-9]{16,})["']?`)
	addressIDRegex    = regexp.MustCompile(`addressId["']?\s*[:=]\s*["']([^"']+)["']`)
	postalCodeRegex   = regexp.MustCompile(`\b(\d{6})\b`)
	phoneRegex        = regexp.MustCompile(`\b(\d{10})\b`)
	walletRegex       = regexp.MustCompile(`(?s)Bisleri Wallet.*?₹\s*([0-9.,]+)`)
	totalRegex        = regexp.MustCompile(`(?s)Total:\s*₹\s*([0-9.,]+)`)
	uuidRegex         = regexp.MustCompile(`(?i)(data-uuid|uuid)[^a-z0-9]{0,10}([a-f0-9]{10,})`)
	qtyRegex          = regexp.MustCompile(`(?i)quantity[^0-9]{0,6}([0-9]{1,2})`)
	cartCountRegex    = regexp.MustCompile(`(?i)Cart\s*(\d+)\s*Items`)
	cartCountAltRegex = regexp.MustCompile(`(?i)\b(\d+)\s*Item\(s\)`)
	productIDRegex    = regexp.MustCompile(`BIS-[A-Z0-9-]+`)
	updateQtyRegex    = regexp.MustCompile(`Cart-UpdateQuantity\?[^"'\s]+`)
)

func ExtractCSRFToken(html string) (string, error) {
	match := csrfRegex.FindStringSubmatch(html)
	if len(match) > 1 {
		return match[1], nil
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return "", err
	}
	if val, ok := doc.Find("input[name=csrf_token]").Attr("value"); ok && val != "" {
		return val, nil
	}
	return "", errors.New("csrf token not found")
}

func ExtractShipmentUUID(html string) (string, error) {
	// Try goquery first for more reliable extraction
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err == nil {
		// Look for hidden input with name="shipmentUUID"
		if val, ok := doc.Find("input[name=shipmentUUID][type=hidden]").Attr("value"); ok && val != "" {
			val = strings.TrimSpace(val)
			// Validate it's a hex string (not an address ID)
			if regexp.MustCompile(`^[a-f0-9]{16,}$`).MatchString(val) {
				return val, nil
			}
		}
		// Also try data-shipment-uuid attribute
		if val, ok := doc.Find("[data-shipment-uuid]").Attr("data-shipment-uuid"); ok && val != "" {
			val = strings.TrimSpace(val)
			if regexp.MustCompile(`^[a-f0-9]{16,}$`).MatchString(val) {
				return val, nil
			}
		}
	}
	
	// Fallback to regex
	match := shipmentUUIDRegex.FindStringSubmatch(html)
	if len(match) > 1 {
		return match[1], nil
	}
	return "", errors.New("shipment UUID not found")
}

func ParseAddressCandidates(html string) ([]AddressCandidate, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, err
	}

	var candidates []AddressCandidate
	selectors := []string{
		"[data-address-id]",
		"[data-addressid]",
		"[data-address_id]",
		".address-card",
		".addressCard",
		".address-book-card",
		".address-book",
	}

	seen := map[*goquery.Selection]bool{}
	for _, sel := range selectors {
		doc.Find(sel).Each(func(_ int, s *goquery.Selection) {
			if seen[s] {
				return
			}
			seen[s] = true
			candidate := AddressCandidate{}
			candidate.RawText = strings.TrimSpace(s.Text())
			if strings.Contains(strings.ToLower(candidate.RawText), "default") {
				candidate.IsDefault = true
			}
			if id, ok := s.Attr("data-address-id"); ok {
				candidate.ID = id
			} else if id, ok := s.Attr("data-addressid"); ok {
				candidate.ID = id
			} else if id, ok := s.Attr("data-address_id"); ok {
				candidate.ID = id
			}
			addr := parseAddressFromText(candidate.RawText)
			candidate.Address = addr
			candidates = append(candidates, candidate)
		})
	}

	if len(candidates) == 0 {
		// fallback: try to find address JSON in HTML
		matches := addressIDRegex.FindAllStringSubmatch(html, -1)
		if len(matches) > 0 {
			for _, m := range matches {
				candidates = append(candidates, AddressCandidate{ID: m[1]})
			}
		}
	}

	return candidates, nil
}

func parseAddressFromText(text string) store.Address {
	addr := store.Address{}
	text = strings.ReplaceAll(text, "\n", " ")
	text = strings.Join(strings.Fields(text), " ")

	if phone := phoneRegex.FindStringSubmatch(text); len(phone) > 1 {
		addr.Phone = phone[1]
	}
	if postal := postalCodeRegex.FindStringSubmatch(text); len(postal) > 1 {
		addr.PostalCode = postal[1]
	}
	// Attempt to split by phone to isolate name/address lines.
	nameAndAddress := text
	if addr.Phone != "" {
		parts := strings.Split(text, addr.Phone)
		if len(parts) > 0 {
			nameAndAddress = strings.TrimSpace(parts[0])
		}
	}
	fields := strings.Fields(nameAndAddress)
	if len(fields) >= 2 {
		addr.FirstName = fields[0]
		addr.LastName = fields[1]
	}
	addr.Address1 = nameAndAddress
	if addr.PostalCode != "" {
		// Heuristic: look for "City, ST 560103" near the postal code.
		idx := strings.Index(text, addr.PostalCode)
		if idx > 0 {
			segment := strings.TrimSpace(text[:idx])
			segmentParts := strings.Split(segment, ",")
			if len(segmentParts) >= 2 {
				addr.City = strings.TrimSpace(segmentParts[len(segmentParts)-2])
				statePart := strings.TrimSpace(segmentParts[len(segmentParts)-1])
				stateFields := strings.Fields(statePart)
				if len(stateFields) > 0 {
					addr.StateCode = stateFields[0]
				}
			}
		}
	}
	if addr.Country == "" {
		addr.Country = "IN"
	}
	return addr
}

func AddressIsComplete(addr store.Address) bool {
	return addr.FirstName != "" && addr.Address1 != "" && addr.City != "" && addr.StateCode != "" && addr.PostalCode != "" && addr.Country != "" && addr.Phone != ""
}

func ExtractWalletBalance(html string) (string, bool) {
	match := walletRegex.FindStringSubmatch(html)
	if len(match) > 1 {
		return "₹" + match[1], true
	}
	return "", false
}

func ExtractOrderTotal(html string) (string, bool) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return "", false
	}

	// Priority 1: Specific class for grand total
	if val := strings.TrimSpace(doc.Find(".grand-total-sum").Text()); val != "" {
		return val, true
	}

	// Priority 2: Regex patterns
	// Try stricter regex first: "Total: ₹ 200" or "Order Total ₹200"
	regexes := []*regexp.Regexp{
		regexp.MustCompile(`(?i)Total\s*:?\s*₹\s*([0-9.,]+)`),
		regexp.MustCompile(`(?i)Payable\s*:?\s*₹\s*([0-9.,]+)`),
		regexp.MustCompile(`(?i)Amount\s*:?\s*₹\s*([0-9.,]+)`),
	}
	
	for _, re := range regexes {
		if match := re.FindStringSubmatch(html); len(match) > 1 {
			return "₹" + match[1], true
		}
	}

	var found string
	// Fallback: Look for elements with "total" in class or text, containing price
	doc.Find("div, span, p, label").EachWithBreak(func(_ int, s *goquery.Selection) bool {
		text := strings.TrimSpace(s.Text())
		if strings.Contains(text, "₹") {
			// Check context
			lower := strings.ToLower(text)
			if strings.Contains(lower, "total") || strings.Contains(lower, "payable") {
				// Extract amount
				re := regexp.MustCompile(`₹\s*([0-9.,]+)`)
				if match := re.FindStringSubmatch(text); len(match) > 1 {
					found = "₹" + match[1]
					return false
				}
			}
		}
		return true
	})

	if found != "" {
		return found, true
	}

	return "", false
}

func ParseINRAmount(value string) (float64, bool) {
	clean := strings.TrimSpace(value)
	clean = strings.TrimPrefix(clean, "₹")
	clean = strings.ReplaceAll(clean, ",", "")
	if clean == "" {
		return 0, false
	}
	amount, err := strconv.ParseFloat(clean, 64)
	if err != nil {
		return 0, false
	}
	return amount, true
}

func ExtractCheckoutForm(html string) (CheckoutForm, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return CheckoutForm{}, err
	}
	var form *goquery.Selection
	bestScore := -1
	doc.Find("form").Each(func(_ int, s *goquery.Selection) {
		score := 0
		action, _ := s.Attr("action")
		actionLower := strings.ToLower(strings.TrimSpace(action))
		if strings.Contains(actionLower, "checkout") || strings.Contains(actionLower, "cart-submitform") {
			score += 2
		}
		id, _ := s.Attr("id")
		name, _ := s.Attr("name")
		if strings.Contains(strings.ToLower(id), "checkout") || strings.Contains(strings.ToLower(name), "checkout") {
			score++
		}
		if s.Find("input[name=csrf_token]").Length() > 0 {
			score++
		}
		hasCheckoutButton := false
		s.Find("input, button").EachWithBreak(func(_ int, el *goquery.Selection) bool {
			n, _ := el.Attr("name")
			if strings.Contains(strings.ToLower(n), "checkout") {
				hasCheckoutButton = true
				return false
			}
			text := strings.ToLower(strings.TrimSpace(el.Text()))
			if strings.Contains(text, "checkout") {
				hasCheckoutButton = true
				return false
			}
			return true
		})
		if hasCheckoutButton {
			score += 3
		}
		if score > bestScore {
			bestScore = score
			form = s
		}
	})
	if form == nil || bestScore <= 0 {
		return CheckoutForm{}, errors.New("checkout form not found")
	}
	action, _ := form.Attr("action")
	method, _ := form.Attr("method")
	if method == "" {
		method = "POST"
	}
	fields := url.Values{}
	form.Find("input").Each(func(_ int, s *goquery.Selection) {
		name, _ := s.Attr("name")
		if strings.TrimSpace(name) == "" {
			return
		}
		typ, _ := s.Attr("type")
		typ = strings.ToLower(strings.TrimSpace(typ))
		if typ == "checkbox" {
			if _, ok := s.Attr("checked"); !ok {
				return
			}
		}
		if typ == "submit" {
			if !strings.Contains(strings.ToLower(name), "checkout") {
				return
			}
		}
		value, _ := s.Attr("value")
		fields.Add(name, value)
	})
	form.Find("button").Each(func(_ int, s *goquery.Selection) {
		name, _ := s.Attr("name")
		if strings.TrimSpace(name) == "" {
			return
		}
		text := strings.ToLower(strings.TrimSpace(s.Text()))
		if strings.Contains(strings.ToLower(name), "checkout") || strings.Contains(text, "checkout") {
			value, _ := s.Attr("value")
			fields.Add(name, value)
		}
	})
	if fields.Get("csrf_token") == "" {
		if token, err := ExtractCSRFToken(html); err == nil {
			fields.Set("csrf_token", token)
		}
	}
	return CheckoutForm{
		Action: strings.TrimSpace(action),
		Method: strings.ToUpper(strings.TrimSpace(method)),
		Fields: fields,
	}, nil
}

func ExtractCheckoutCandidates(html string) []CheckoutCandidate {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var candidates []CheckoutCandidate
	addCandidate := func(action, method, source string) {
		action = strings.TrimSpace(action)
		if action == "" {
			return
		}
		method = strings.ToUpper(strings.TrimSpace(method))
		if method == "" {
			method = http.MethodGet
		}
		key := method + " " + action
		if seen[key] {
			return
		}
		seen[key] = true
		candidates = append(candidates, CheckoutCandidate{
			Action: action,
			Method: method,
			Source: source,
		})
	}

	doc.Find("form").Each(func(_ int, s *goquery.Selection) {
		action, _ := s.Attr("action")
		actionLower := strings.ToLower(strings.TrimSpace(action))
		if strings.Contains(actionLower, "checkout") || strings.Contains(actionLower, "cart-submitform") || strings.Contains(actionLower, "checkout-begin") {
			method, _ := s.Attr("method")
			addCandidate(action, method, "form")
		}
	})

	doc.Find("a[href]").Each(func(_ int, s *goquery.Selection) {
		href, _ := s.Attr("href")
		hrefLower := strings.ToLower(strings.TrimSpace(href))
		if strings.Contains(hrefLower, "checkout") {
			addCandidate(href, http.MethodGet, "link")
		}
	})

	doc.Find("button, input").Each(func(_ int, s *goquery.Selection) {
		for _, attr := range []string{"data-url", "data-action", "formaction", "href"} {
			if val, ok := s.Attr(attr); ok {
				valLower := strings.ToLower(strings.TrimSpace(val))
				if strings.Contains(valLower, "checkout") {
					method, _ := s.Attr("data-method")
					addCandidate(val, method, "data-attr")
				}
			}
		}
	})

	endpointRegexes := []*regexp.Regexp{
		regexp.MustCompile(`(?i)(/on/demandware\.store/[^"'\\s]*Checkout-[A-Za-z]+[^"'\\s]*)`),
		regexp.MustCompile(`(?i)(/checkout[^"'\\s]*)`),
	}
	for _, re := range endpointRegexes {
		matches := re.FindAllStringSubmatch(html, -1)
		for _, m := range matches {
			if len(m) > 1 {
				addCandidate(m[1], http.MethodGet, "regex")
			} else if len(m) > 0 {
				addCandidate(m[0], http.MethodGet, "regex")
			}
		}
	}

	return candidates
}

func ExtractSelectedCity(html string) (string, bool) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return "", false
	}
	sel := doc.Find("select#citySelect option[selected]").First()
	if sel.Length() == 0 {
		return "", false
	}
	val, _ := sel.Attr("value")
	val = strings.TrimSpace(val)
	if val == "" {
		return "", false
	}
	return val, true
}

func ExtractCityOptions(html string) []string {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var options []string
	doc.Find("select#citySelect option").Each(func(_ int, s *goquery.Selection) {
		val, _ := s.Attr("value")
		val = strings.TrimSpace(val)
		if val == "" {
			return
		}
		if seen[val] {
			return
		}
		seen[val] = true
		options = append(options, val)
	})
	return options
}

type CartItem struct {
	ProductID string
	UUID      string
	Quantity  int
}

func ExtractCartItems(html string) []CartItem {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil
	}
	var items []CartItem
	doc.Find("[data-uuid]").Each(func(_ int, s *goquery.Selection) {
		uuid, _ := s.Attr("data-uuid")
		uuid = strings.TrimSpace(uuid)
		if uuid == "" {
			return
		}
		productID := extractProductIDFromSelection(s)
		qty := extractQuantityFromSelection(s)
		items = append(items, CartItem{
			ProductID: productID,
			UUID:      uuid,
			Quantity:  qty,
		})
	})
	if len(items) > 0 {
		return items
	}
	// Action URL fallback (more reliable than raw regex).
	actionItems := extractCartItemsFromActionURLs(html)
	if len(actionItems) > 0 {
		return actionItems
	}
	// Regex fallback (less reliable).
	idx := 0
	for {
		pos := uuidRegex.FindStringSubmatchIndex(html[idx:])
		if pos == nil {
			break
		}
		match := uuidRegex.FindStringSubmatch(html[idx:])
		if len(match) > 2 {
			windowStart := idx + pos[0] - 800
			if windowStart < 0 {
				windowStart = 0
			}
			windowEnd := idx + pos[1] + 800
			if windowEnd > len(html) {
				windowEnd = len(html)
			}
			window := html[windowStart:windowEnd]
			productID := ""
			if idMatch := productIDRegex.FindStringSubmatch(window); len(idMatch) > 0 {
				productID = idMatch[0]
			}
			qty := 0
			qtyMatch := qtyRegex.FindStringSubmatch(window)
			if len(qtyMatch) > 1 {
				qty = atoiSafe(qtyMatch[1])
			}
			items = append(items, CartItem{
				ProductID: productID,
				UUID:      match[2],
				Quantity:  qty,
			})
		}
		idx = idx + pos[1]
	}
	return items
}

func ExtractCartCount(html string) (int, bool) {
	match := cartCountRegex.FindStringSubmatch(html)
	if len(match) > 1 {
		return atoiSafe(match[1]), true
	}
	match = cartCountAltRegex.FindStringSubmatch(html)
	if len(match) > 1 {
		return atoiSafe(match[1]), true
	}
	return 0, false
}

func ExtractCartItem(html, productID string) (string, int, bool) {
	items := ExtractCartItems(html)
	for _, item := range items {
		if strings.EqualFold(item.ProductID, productID) {
			return item.UUID, item.Quantity, true
		}
	}
	return "", 0, false
}

func atoiSafe(value string) int {
	n := 0
	for _, r := range value {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return n
}

func extractProductIDFromSelection(s *goquery.Selection) string {
	candidates := []string{"data-pid", "data-product-id", "data-productid", "data-itemid", "data-product"}
	for _, attr := range candidates {
		if val, ok := s.Attr(attr); ok && val != "" {
			return strings.TrimSpace(val)
		}
	}
	var found string
	s.Find("a[href]").EachWithBreak(func(_ int, link *goquery.Selection) bool {
		href, _ := link.Attr("href")
		href = strings.TrimSpace(href)
		if href == "" {
			return true
		}
		if match := productIDRegex.FindStringSubmatch(href); len(match) > 0 {
			found = match[0]
			return false
		}
		return true
	})
	if found != "" {
		return found
	}
	if html, err := s.Html(); err == nil {
		if match := productIDRegex.FindStringSubmatch(html); len(match) > 0 {
			return match[0]
		}
	}
	return ""
}

func extractQuantityFromSelection(s *goquery.Selection) int {
	if input := s.Find("input"); input.Length() > 0 {
		if val, ok := input.Attr("value"); ok {
			if qty := atoiSafe(val); qty > 0 {
				return qty
			}
		}
	}
	if html, err := s.Html(); err == nil {
		if match := qtyRegex.FindStringSubmatch(html); len(match) > 1 {
			return atoiSafe(match[1])
		}
	}
	text := strings.ToLower(strings.TrimSpace(s.Text()))
	if match := qtyRegex.FindStringSubmatch(text); len(match) > 1 {
		return atoiSafe(match[1])
	}
	return 0
}

func extractCartItemsFromActionURLs(html string) []CartItem {
	var items []CartItem
	matches := updateQtyRegex.FindAllString(html, -1)
	for _, m := range matches {
		parts := strings.SplitN(m, "?", 2)
		if len(parts) != 2 {
			continue
		}
		values, err := url.ParseQuery(parts[1])
		if err != nil {
			continue
		}
		pid := values.Get("pid")
		uuid := values.Get("uuid")
		qty := atoiSafe(values.Get("quantity"))
		if pid == "" || uuid == "" {
			continue
		}
		items = append(items, CartItem{
			ProductID: pid,
			UUID:      uuid,
			Quantity:  qty,
		})
	}
	return items
}
