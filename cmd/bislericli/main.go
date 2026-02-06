package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"bislericli/internal/auth"
	"bislericli/internal/bisleri"
	"bislericli/internal/config"
	"bislericli/internal/debug"
	"bislericli/internal/format"
	"bislericli/internal/store"
)

const (
	productID20L = "BIS-20LTR01-90"
)

var (
	version = "dev"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) < 2 {
		printUsage()
		return nil
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "auth":
		return runAuth(args)
	case "profile":
		return runProfile(args)
	case "order":
		return runOrder(args)
	case "orders":
		return runOrders(args)
	case "stats":
		return runStats(args)
	case "sync":
		return runSync(args)
	case "config":
		return runConfig(args)
	case "schedule":
		return runSchedule(args)
	case "version":
		fmt.Println(version)
		return nil
	case "debug":
		return runDebug(args)
	case "-h", "--help", "help":
		printUsage()
		return nil
	default:
		printUsage()
		return fmt.Errorf("unknown command: %s", cmd)
	}
}

func printUsage() {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Println("bislericli - Bisleri Customer CLI Tool")
	fmt.Println("\nUsage:")
	fmt.Println("  bislericli <command> [command flags]")

	fmt.Println("\nAuthentication:")
	fmt.Fprintln(w, "  auth login\tInteractive login to Bisleri account")
	fmt.Fprintln(w, "  auth logout\tLogout from the current session")
	fmt.Fprintln(w, "  auth status\tCheck current login status")
	fmt.Fprintln(w, "  profile list\tList all available profiles")
	fmt.Fprintln(w, "  profile use\tSwitch to a different profile")
	w.Flush()

	fmt.Println("\nOrders & Stats:")
	fmt.Fprintln(w, "  order\tPlace a new water can order")
	fmt.Fprintln(w, "  orders\tView your order history")
	fmt.Fprintln(w, "  sync\tFetch and cache recent data from server")
	fmt.Fprintln(w, "  stats\tAnalyze spending habits and patterns")
	fmt.Fprintln(w, "  schedule\tManage recurring order schedules")
	w.Flush()

	fmt.Println("\nConfiguration:")
	fmt.Fprintln(w, "  config show\tDisplay current configuration")
	w.Flush()
	fmt.Println("\nFlags:")
	fmt.Println("  version            Show version information")
	fmt.Println("  --help             Show this help message")
	fmt.Println()
	fmt.Println("Note: flags like --profile are command-specific.")
	fmt.Println("Run 'bislericli <command> --help' for specific command usage.")
}

func runAuth(args []string) error {
	if len(args) < 1 || isHelpToken(args[0]) {
		printAuthUsage()
		return nil
	}
	sub := args[0]
	subArgs := args[1:]

	switch sub {
	case "login":
		fs := flag.NewFlagSet("auth login", flag.ContinueOnError)
		profileName := fs.String("profile", "", "profile name")
		method := fs.String("method", "otp", "login method: otp (default) or browser")
		phone := fs.String("phone", "", "phone number (10 digits, will prompt if not provided)")
		if err := fs.Parse(subArgs); err != nil {
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
		existingProfile, profilePath, err := loadOrCreateProfile(name)
		if err != nil {
			return err
		}

		var cookies []store.Cookie
		var phoneNumber string

		switch *method {
		case "browser":
			// Use browser-based login
			cookies, err = auth.Login(context.Background())
			if err != nil {
				return err
			}
		case "otp":
			fallthrough
		default:
			// Use OTP-based login (default)
			phoneNumber = *phone
			if phoneNumber == "" && existingProfile.PhoneNumber != "" {
				// Use saved phone number
				fmt.Printf("Phone number [%s]: ", existingProfile.PhoneNumber)
				reader := bufio.NewReader(os.Stdin)
				input, _ := reader.ReadString('\n')
				input = strings.TrimSpace(input)
				if input == "" {
					phoneNumber = existingProfile.PhoneNumber
				} else {
					phoneNumber = input
				}
			}
			if phoneNumber == "" {
				// Prompt for phone number
				fmt.Print("Phone number: ")
				reader := bufio.NewReader(os.Stdin)
				input, _ := reader.ReadString('\n')
				phoneNumber = strings.TrimSpace(input)
			}
			// Clean phone number (remove spaces, dashes first)
			phoneNumber = strings.ReplaceAll(phoneNumber, " ", "")
			phoneNumber = strings.ReplaceAll(phoneNumber, "-", "")
			// Only strip country code if number is too long
			if strings.HasPrefix(phoneNumber, "+91") && len(phoneNumber) == 13 {
				phoneNumber = strings.TrimPrefix(phoneNumber, "+91")
			} else if strings.HasPrefix(phoneNumber, "91") && len(phoneNumber) == 12 {
				phoneNumber = strings.TrimPrefix(phoneNumber, "91")
			}

			if len(phoneNumber) != 10 {
				return fmt.Errorf("invalid phone number: must be 10 digits, got %d", len(phoneNumber))
			}

			cookies, err = auth.LoginWithOTP(context.Background(), phoneNumber)
			if err != nil {
				return fmt.Errorf("login failed: %w", err)
			}
		}

		profile := store.Profile{
			Name:        name,
			Cookies:     cookies,
			PhoneNumber: phoneNumber,
			LastLogin:   time.Now(),
		}

		// Preserve existing profile data
		if existingProfile.AddressID != "" {
			profile.AddressID = existingProfile.AddressID
		}
		if existingProfile.Address != nil {
			profile.Address = existingProfile.Address
		}
		if existingProfile.PreferredCity != "" {
			profile.PreferredCity = existingProfile.PreferredCity
		}
		if phoneNumber == "" && existingProfile.PhoneNumber != "" {
			profile.PhoneNumber = existingProfile.PhoneNumber
		}

		if err := store.SaveProfile(profilePath, profile); err != nil {
			return err
		}
		if err := tryCaptureAddress(profilePath, &profile); err == nil {
			_ = store.SaveProfile(profilePath, profile)
		}
		cfg.CurrentProfile = name
		if err := config.SaveGlobalConfig(cfg); err != nil {
			return err
		}
		fmt.Println("Login captured for profile:", name)
		return nil
	case "status":
		fs := flag.NewFlagSet("auth status", flag.ContinueOnError)
		profileName := fs.String("profile", "", "profile name")
		if err := fs.Parse(subArgs); err != nil {
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
		fmt.Println(format.KeyValue("Profile", profile.Name))
		fmt.Println(format.KeyValue("Last login", format.Timestamp(profile.LastLogin)))
		fmt.Println(format.KeyValue("Cookies", fmt.Sprintf("%d", len(profile.Cookies))))
		if profile.Address != nil {
			fmt.Println(format.KeyValue("Address", profile.Address.Address1))
		} else {
			fmt.Println(format.KeyValue("Address", "not set"))
		}
		if profile.PhoneNumber != "" {
			fmt.Println(format.KeyValue("Phone", profile.PhoneNumber))
		}
		return nil
	case "logout":
		fs := flag.NewFlagSet("auth logout", flag.ContinueOnError)
		profileName := fs.String("profile", "", "profile name")
		if err := fs.Parse(subArgs); err != nil {
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
		profile, profilePath, err := loadOrCreateProfile(name)
		if err != nil {
			return err
		}
		if len(profile.Cookies) > 0 {
			jar, err := bisleri.JarFromCookies(profile.Cookies)
			if err != nil {
				return err
			}
			client := bisleri.NewClient(&http.Client{Jar: jar, Timeout: 20 * time.Second}, log.New(os.Stderr, "bisleri: ", log.LstdFlags))
			if err := client.Logout(context.Background()); err != nil {
				fmt.Fprintln(os.Stderr, "Warning: remote logout failed:", err)
			}
		}
		profile.Cookies = nil
		if err := store.SaveProfile(profilePath, profile); err != nil {
			return err
		}
		fmt.Println("Logged out profile:", name)
		return nil
	default:
		fmt.Printf("Unknown auth subcommand: %s\n", sub)
		printAuthUsage()
		return nil
	}
}

func runProfile(args []string) error {
	if len(args) < 1 || isHelpToken(args[0]) {
		printProfileUsage()
		return nil
	}
	sub := args[0]
	subArgs := args[1:]

	switch sub {
	case "list":
		dir, err := config.ProfilesDir()
		if err != nil {
			return err
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		var names []string
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			if strings.HasSuffix(entry.Name(), ".json") {
				names = append(names, strings.TrimSuffix(entry.Name(), ".json"))
			}
		}
		sort.Strings(names)
		if len(names) == 0 {
			fmt.Println("No profiles found. Run: bislericli auth login")
			return nil
		}
		for _, name := range names {
			fmt.Println(name)
		}
		return nil
	case "use":
		if len(subArgs) < 1 {
			return errors.New("profile name required")
		}
		name := subArgs[0]
		if _, _, err := loadOrCreateProfile(name); err != nil {
			return err
		}
		cfg, err := config.LoadGlobalConfig()
		if err != nil {
			return err
		}
		cfg.CurrentProfile = name
		if err := config.SaveGlobalConfig(cfg); err != nil {
			return err
		}
		fmt.Println("Current profile set to:", name)
		return nil
	default:
		fmt.Printf("Unknown profile subcommand: %s\n", sub)
		printProfileUsage()
		return nil
	}
}

func runOrder(args []string) error {
	fs := flag.NewFlagSet("order", flag.ContinueOnError)
	profileName := fs.String("profile", "", "Profile name to use (default: current/default)")
	quantity := fs.Int("qty", 0, "Number of 20L jars to order")
	returnJars := fs.Int("return", -1, "Number of empty jars to return (default: matches order qty)")
	allowExtra := fs.Bool("allow-extra", false, "Proceed even if cart contains other items")
	debug := fs.Bool("debug", false, "Enable verbose debug logging")
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
	profile, profilePath, err := loadOrCreateProfile(name)
	if err != nil {
		return err
	}
	if len(profile.Cookies) == 0 {
		return errors.New("no cookies in profile; run 'bislericli auth login'")
	}

	if *quantity == 0 {
		*quantity = cfg.Defaults.OrderQuantity
	}
	if *quantity <= 0 {
		return errors.New("quantity must be a positive number")
	}
	if *returnJars < 0 {
		*returnJars = *quantity
	}
	if *returnJars > *quantity {
		return fmt.Errorf("return jars (%d) cannot exceed order quantity (%d)", *returnJars, *quantity)
	}

	fmt.Printf("Placing order: %d jar(s), returning %d jar(s)\n", *quantity, *returnJars)

	jar, err := bisleri.JarFromCookies(profile.Cookies)
	if err != nil {
		return err
	}
	client := bisleri.NewClient(&http.Client{Jar: jar, Timeout: 40 * time.Second}, log.New(os.Stderr, "bisleri: ", log.LstdFlags))
	if *debug {
		client.Debug = true
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	fmt.Println("Checking session...")
	if err := client.VerifyAuthenticated(ctx); err != nil {
		return err
	}

	fmt.Println("Preparing cart...")
	cartHTML, cartErr := client.FetchCartPage(ctx)
	if cartErr == nil {
		updatedHTML, err := ensureCityLocation(ctx, client, profilePath, &profile, cartHTML)
		if err != nil {
			return err
		}
		cartHTML = updatedHTML
	}
	if cartErr == nil {
		cartItems := bisleri.ExtractCartItems(cartHTML)
		if count, ok := bisleri.ExtractCartCount(cartHTML); ok && count > 0 && len(cartItems) == 0 {
			return errors.New("unable to parse cart items; please clear cart or try again")
		}
		extraItems := filterExtraItems(cartItems, productID20L)
		if len(extraItems) > 0 && !*allowExtra {
			return fmt.Errorf("cart contains other items; clear cart or pass --allow-extra (items: %s)", strings.Join(extraItems, ", "))
		}
		if uuid, existingQty, ok := bisleri.ExtractCartItem(cartHTML, productID20L); ok && uuid != "" {
			if existingQty != *quantity {
				fmt.Println("Updating cart quantity...")
				if err := client.UpdateQuantity(ctx, productID20L, uuid, *quantity); err != nil {
					return err
				}
			} else {
				fmt.Println("Cart already at desired quantity.")
			}
		} else {
			if len(cartItems) > 0 && !*allowExtra {
				return errors.New("cart is not empty; clear cart or pass --allow-extra")
			}
			fmt.Println("Adding product to cart...")
			if err := client.AddProduct(ctx, productID20L, *quantity); err != nil {
				return err
			}
			if err := confirmCartQuantity(ctx, client, productID20L, *quantity, *allowExtra); err != nil {
				return err
			}
		}
	} else {
		if errors.Is(cartErr, bisleri.ErrNotAuthenticated) {
			return cartErr
		}
		fmt.Fprintln(os.Stderr, "Warning: unable to fetch cart; proceeding to add product:", cartErr)
		fmt.Println("Adding product to cart...")
		if err := client.AddProduct(ctx, productID20L, *quantity); err != nil {
			return err
		}
		if err := confirmCartQuantity(ctx, client, productID20L, *quantity, *allowExtra); err != nil {
			return err
		}
	}
	fmt.Println("Setting return jars...")
	if err := client.UpdateJarQuantity(ctx, *returnJars); err != nil {
		return err
	}

	// Give the server time to process the cart update
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(500 * time.Millisecond):
	}

	if profile.Address != nil && profile.AddressID != "" {
		addr := *profile.Address
		if profile.PreferredCity != "" && !strings.EqualFold(addr.City, profile.PreferredCity) {
			addr.City = profile.PreferredCity
		}
		if addr.City == "" {
			addr.City = profile.PreferredCity
		}
		normalizeStateCode(&addr)
		if addr.Country == "" {
			addr.Country = "IN"
		}
		if addressReadyForLocation(addr) {
			if err := client.SetSavedAddressLocation(ctx, addr, profile.AddressID); err != nil && *debug {
				fmt.Fprintln(os.Stderr, "bisleri: set saved address warning:", err)
			}
		} else if *debug {
			fmt.Fprintln(os.Stderr, "bisleri: saved address location skipped (missing fields)")
		}
	}

	// Give the server a moment to stabilize before checkout
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(300 * time.Millisecond):
	}

	fmt.Println("Fetching shipping details...")
	// Try BeginCheckout first, with retry logic
	var beginErr error
	for attempt := 1; attempt <= 2; attempt++ {
		if err := client.BeginCheckout(ctx); err != nil {
			beginErr = err
			if *debug {
				fmt.Fprintf(os.Stderr, "bisleri: checkout init attempt %d warning: %v\n", attempt, err)
			}
			if attempt < 2 {
				// Brief delay before retry
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(time.Second):
				}
			}
		} else {
			beginErr = nil
			break
		}
	}

	shippingHTML, err := client.FetchShippingPage(ctx)
	if err != nil {
		var statusErr *bisleri.HTTPStatusError
		if errors.As(err, &statusErr) && statusErr.StatusCode == http.StatusInternalServerError {
			fmt.Println("Shipping page returned 500. Initializing checkout and retrying...")
			if retryErr := client.BeginCheckout(ctx); retryErr != nil && *debug {
				fmt.Fprintln(os.Stderr, "bisleri: checkout retry warning:", retryErr)
			}
			shippingHTML, err = client.FetchShippingPage(ctx)
		}
		if err != nil {
			if beginErr != nil {
				return fmt.Errorf("%w (checkout init error: %v)", err, beginErr)
			}
			return err
		}
	}
	csrfToken, err := bisleri.ExtractCSRFToken(shippingHTML)
	if err != nil {
		return fmt.Errorf("failed to parse csrf token (session expired?): %w", err)
	}
	shipmentUUID, err := bisleri.ExtractShipmentUUID(shippingHTML)
	if err != nil {
		// Debug: save shipping HTML to file ONLY if debug is enabled
		if *debug {
			debugFile := "/tmp/shipping_page_debug.html"
			if writeErr := os.WriteFile(debugFile, []byte(shippingHTML), 0600); writeErr == nil {
				fmt.Fprintf(os.Stderr, "Debug: Shipping HTML saved to %s\n", debugFile)
			}
		}
		return fmt.Errorf("failed to parse shipment UUID: %w", err)
	}

	if profile.Address == nil || profile.AddressID == "" {
		candidates, err := bisleri.ParseAddressCandidates(shippingHTML)
		if err != nil {
			return err
		}
		if len(candidates) == 0 {
			return errors.New("no address found in account; set a default address on bisleri.com and retry")
		}
		choice := selectAddress(candidates)
		profile.AddressID = choice.ID
		profile.Address = &choice.Address
		profile.AddressSource = "shipping-page"
		ensureAddressComplete(profile.Address)
		if err := store.SaveProfile(profilePath, profile); err != nil {
			return err
		}
	}

	if !bisleri.AddressIsComplete(*profile.Address) {
		ensureAddressComplete(profile.Address)
		if err := store.SaveProfile(profilePath, profile); err != nil {
			return err
		}
	}

	fmt.Println("Submitting shipping info...")
	if err := client.SubmitShipping(ctx, shipmentUUID, csrfToken, cfg.Defaults.Timeslot, *profile.Address, profile.AddressID); err != nil {
		return err
	}

	fmt.Println("Fetching payment page...")
	paymentHTML, err := client.FetchPaymentPage(ctx)
	if err != nil {
		return err
	}
	if balance, ok := bisleri.ExtractWalletBalance(paymentHTML); ok {
		fmt.Println(format.KeyValue("Wallet balance", balance))
	}
	if total, ok := bisleri.ExtractOrderTotal(paymentHTML); ok {
		fmt.Println(format.KeyValue("Order total", total))
	}
	// Check order total and wallet balance
	if total, okTotal := bisleri.ExtractOrderTotal(paymentHTML); okTotal {
		if totalAmount, okTot := bisleri.ParseINRAmount(total); okTot {
			if totalAmount <= 0 {
				if *debug {
					debugFile := "/tmp/payment_page_fail_total.html"
					if writeErr := os.WriteFile(debugFile, []byte(paymentHTML), 0600); writeErr == nil {
						fmt.Fprintf(os.Stderr, "Debug: Payment HTML saved to %s\n", debugFile)
					}
				}
				return fmt.Errorf("invalid order total detected (%s); check debug html", total)
			}

			// Balance check
			if balance, okBal := bisleri.ExtractWalletBalance(paymentHTML); okBal {
				if balAmount, okBalPars := bisleri.ParseINRAmount(balance); okBalPars {
					if balAmount < totalAmount {
						return fmt.Errorf("insufficient wallet balance (%s) for order total (%s)", balance, total)
					}
				}
			} else {
				fmt.Println("Warning: could not detect wallet balance")
			}
		} else {
			return fmt.Errorf("failed to parse order total amount: %s", total)
		}
	} else {
		if *debug {
			debugFile := "/tmp/payment_page_no_total.html"
			if writeErr := os.WriteFile(debugFile, []byte(paymentHTML), 0600); writeErr == nil {
				fmt.Fprintf(os.Stderr, "Debug: Payment HTML saved to %s\n", debugFile)
			}
		}
		return errors.New("failed to detect order total on payment page")
	}
	paymentCSRF, err := bisleri.ExtractCSRFToken(paymentHTML)
	if err != nil {
		paymentCSRF = csrfToken
	}
	fmt.Println("Submitting payment (Bisleri Wallet)...")
	if err := client.SubmitPayment(ctx, shipmentUUID, paymentCSRF, *profile.Address); err != nil {
		return err
	}
	fmt.Println("Placing order...")
	orderID, err := client.PlaceOrder(ctx)
	if err != nil {
		return err
	}
	if orderID == "" {
		return errors.New("order placement did not return a valid order ID; check wallet or order history")
	} else {
		fmt.Println("Order placed:", orderID)
		profile.LastOrder = &store.OrderInfo{OrderID: orderID, PlacedAt: time.Now()}
		if err := store.SaveProfile(profilePath, profile); err != nil {
			fmt.Fprintln(os.Stderr, "Warning: failed to save order info:", err)
		}
	}
	if postPaymentHTML, err := client.FetchPaymentPage(ctx); err == nil {
		if balance, ok := bisleri.ExtractWalletBalance(postPaymentHTML); ok {
			fmt.Println(format.KeyValue("Wallet balance (post-order)", balance))
		}
	}

	return nil
}

func runConfig(args []string) error {
	if len(args) < 1 || isHelpToken(args[0]) {
		printConfigUsage()
		return nil
	}
	if args[0] != "show" {
		fmt.Printf("Unknown config subcommand: %s\n", args[0])
		printConfigUsage()
		return nil // Return nil to avoid generic error printing
	}
	dir, err := config.ConfigDir()
	if err != nil {
		return err
	}
	cfgPath, err := config.ConfigFilePath()
	if err != nil {
		return err
	}
	fmt.Println(format.KeyValue("Config dir", dir))
	fmt.Println(format.KeyValue("Config file", cfgPath))
	profilesDir, err := config.ProfilesDir()
	if err != nil {
		return err
	}
	fmt.Println(format.KeyValue("Profiles", profilesDir))
	return nil
}

func runSchedule(args []string) error {
	if len(args) > 0 && isHelpToken(args[0]) {
		printScheduleUsage()
		return nil
	}
	cfg, err := config.LoadGlobalConfig()
	if err != nil {
		return err
	}
	fmt.Println("Schedule:", cfg.Defaults.Schedule)
	fmt.Println("Default quantity:", cfg.Defaults.OrderQuantity)
	fmt.Println("Default return jars:", cfg.Defaults.ReturnJars)
	return nil
}

func runDebug(args []string) error {
	if len(args) < 1 || isHelpToken(args[0]) {
		printDebugUsage()
		return nil
	}
	sub := args[0]

	switch sub {
	case "order":
		cfg, err := config.LoadGlobalConfig()
		if err != nil {
			return err
		}
		name := resolveProfileName("", cfg)
		profile, _, err := loadOrCreateProfile(name)
		if err != nil {
			return err
		}
		if len(profile.Cookies) == 0 {
			return errors.New("no cookies in profile; run 'bislericli auth login'")
		}

		fmt.Println("Starting debug order flow for profile:", name)
		return debug.RunOrderDebug(context.Background(), profile)
	default:
		fmt.Printf("Unknown debug subcommand: %s\n", sub)
		printDebugUsage()
		return nil
	}
}

func isHelpToken(token string) bool {
	switch token {
	case "help", "-h", "--help":
		return true
	default:
		return false
	}
}

func printAuthUsage() {
	fmt.Println("Usage: bislericli auth <subcommand> [flags]")
	fmt.Println("\nAvailable subcommands:")
	fmt.Println("  login    Interactive login to Bisleri account")
	fmt.Println("  logout   Logout from the current session")
	fmt.Println("  status   Check current login status")
}

func printProfileUsage() {
	fmt.Println("Usage: bislericli profile <subcommand>")
	fmt.Println("\nAvailable subcommands:")
	fmt.Println("  list   List all available profiles")
	fmt.Println("  use    Switch to a different profile")
}

func printConfigUsage() {
	fmt.Println("Usage: bislericli config <subcommand>")
	fmt.Println("\nAvailable subcommands:")
	fmt.Println("  show   Display current configuration")
}

func printScheduleUsage() {
	fmt.Println("Usage: bislericli schedule")
	fmt.Println()
	fmt.Println("Show current default scheduling values.")
}

func printDebugUsage() {
	fmt.Println("Usage: bislericli debug <subcommand>")
	fmt.Println("\nAvailable subcommands:")
	fmt.Println("  order   Start debug order flow")
}

func resolveProfileName(flagValue string, cfg config.GlobalConfig) string {
	if flagValue != "" {
		return flagValue
	}
	if cfg.CurrentProfile == "" {
		return "default"
	}
	return cfg.CurrentProfile
}

func selectAddress(candidates []bisleri.AddressCandidate) bisleri.AddressCandidate {
	if len(candidates) == 1 {
		return candidates[0]
	}
	for _, c := range candidates {
		if c.IsDefault {
			return c
		}
	}
	fmt.Println("Multiple addresses found. Which address should be set as default?")
	for i, c := range candidates {
		label := strings.TrimSpace(c.RawText)
		if label == "" {
			label = c.ID
		}
		fmt.Printf("  %d) %s\n", i+1, label)
	}
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Printf("Choose [1-%d]: ", len(candidates))
		line, _ := reader.ReadString('\n')
		line = strings.TrimSpace(line)
		idx, err := parseIndex(line, len(candidates))
		if err == nil {
			return candidates[idx]
		}
	}
}

func resolveCity(profile store.Profile, options []string) string {
	if len(options) > 0 {
		if match, ok := matchCityOption(profile.PreferredCity, options); ok {
			return match
		}
		if profile.Address != nil {
			if match, ok := matchCityOption(profile.Address.City, options); ok {
				return match
			}
		}
		return selectCity(options)
	}
	if profile.PreferredCity != "" {
		return profile.PreferredCity
	}
	if profile.Address != nil && profile.Address.City != "" {
		return profile.Address.City
	}
	return selectCity(nil)
}

func selectCity(options []string) string {
	reader := bufio.NewReader(os.Stdin)
	if len(options) == 0 {
		fmt.Print("Enter delivery city: ")
		line, _ := reader.ReadString('\n')
		return strings.TrimSpace(line)
	}
	fmt.Println("Select delivery city:")
	for i, city := range options {
		fmt.Printf("  %d) %s\n", i+1, city)
	}
	for {
		fmt.Printf("Choose [1-%d] or type city name: ", len(options))
		line, _ := reader.ReadString('\n')
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if idx, err := parseIndex(line, len(options)); err == nil {
			return options[idx]
		}
		if match, ok := matchCityOption(line, options); ok {
			return match
		}
		return line
	}
}

func matchCityOption(candidate string, options []string) (string, bool) {
	candidate = strings.TrimSpace(candidate)
	if candidate == "" || len(options) == 0 {
		return "", false
	}
	for _, opt := range options {
		if strings.EqualFold(opt, candidate) {
			return opt, true
		}
	}
	aliases := map[string]string{
		"bangalore": "bengaluru",
		"bengaluru": "bangalore",
		"gurgaon":   "gurugram",
		"gurugram":  "gurgaon",
		"bombay":    "mumbai",
	}
	if mapped, ok := aliases[strings.ToLower(candidate)]; ok {
		for _, opt := range options {
			if strings.EqualFold(opt, mapped) {
				return opt, true
			}
		}
	}
	lowerCandidate := strings.ToLower(candidate)
	var matches []string
	for _, opt := range options {
		lowerOpt := strings.ToLower(opt)
		if strings.Contains(lowerCandidate, lowerOpt) || strings.Contains(lowerOpt, lowerCandidate) {
			matches = append(matches, opt)
		}
	}
	if len(matches) == 1 {
		return matches[0], true
	}
	return "", false
}

func normalizeStateCode(addr *store.Address) {
	if addr == nil {
		return
	}
	if len(addr.StateCode) == 2 {
		return
	}
	if addr.Address1 == "" {
		return
	}
	re := regexp.MustCompile(`\\b[A-Z]{2}\\b`)
	matches := re.FindAllString(addr.Address1, -1)
	for _, m := range matches {
		if m == "IN" {
			continue
		}
		addr.StateCode = m
		return
	}
}

func addressReadyForLocation(addr store.Address) bool {
	return addr.Address1 != "" && addr.City != "" && addr.StateCode != "" && addr.PostalCode != "" && addr.Country != ""
}

func parseIndex(value string, max int) (int, error) {
	if value == "" {
		return 0, errors.New("empty")
	}
	n := 0
	for _, r := range value {
		if r < '0' || r > '9' {
			return 0, errors.New("not a number")
		}
		n = n*10 + int(r-'0')
	}
	if n < 1 || n > max {
		return 0, errors.New("out of range")
	}
	return n - 1, nil
}

func ensureAddressComplete(addr *store.Address) {
	reader := bufio.NewReader(os.Stdin)
	prompt := func(label string, current *string) {
		if *current != "" {
			return
		}
		fmt.Printf("%s: ", label)
		line, _ := reader.ReadString('\n')
		*current = strings.TrimSpace(line)
	}

	prompt("First name", &addr.FirstName)
	prompt("Last name", &addr.LastName)
	prompt("Address line 1", &addr.Address1)
	if addr.Address2 == "" {
		prompt("Address line 2 (optional)", &addr.Address2)
	}
	if addr.Floor == "" {
		prompt("Floor (optional)", &addr.Floor)
	}
	if addr.NearByLandmark == "" {
		prompt("Landmark (optional)", &addr.NearByLandmark)
	}
	prompt("City", &addr.City)
	prompt("State code (e.g. KA)", &addr.StateCode)
	prompt("Postal code", &addr.PostalCode)
	prompt("Phone", &addr.Phone)
	if addr.Country == "" {
		addr.Country = "IN"
	}
	if addr.Latitude == "" {
		prompt("Latitude (optional)", &addr.Latitude)
	}
	if addr.Longitude == "" {
		prompt("Longitude (optional)", &addr.Longitude)
	}
}

func filterExtraItems(items []bisleri.CartItem, productID string) []string {
	allowed := map[string]bool{
		strings.ToLower(productID):                         true,
		strings.ToLower("Bis-20LTREmpty-Product"):          true,
		strings.ToLower("Bis-20LTRDeposit-Amount-Product"): true,
	}
	var extras []string
	for _, item := range items {
		id := strings.ToLower(strings.TrimSpace(item.ProductID))
		if id == "" {
			extras = append(extras, "unknown-item")
			continue
		}
		if !allowed[id] {
			extras = append(extras, item.ProductID)
		}
	}
	return extras
}

func confirmCartQuantity(ctx context.Context, client *bisleri.Client, productID string, quantity int, allowExtra bool) error {
	const maxAttempts = 4
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		cartHTML, err := client.FetchCartPage(ctx)
		if err != nil {
			if errors.Is(err, bisleri.ErrNotAuthenticated) {
				return err
			}
			lastErr = err
		} else {
			items := bisleri.ExtractCartItems(cartHTML)
			if count, ok := bisleri.ExtractCartCount(cartHTML); ok && count > 0 && len(items) == 0 {
				lastErr = errors.New("unable to parse cart items")
			} else {
				extraItems := filterExtraItems(items, productID)
				if len(extraItems) > 0 && !allowExtra {
					return fmt.Errorf("cart contains other items; clear cart or pass --allow-extra (items: %s)", strings.Join(extraItems, ", "))
				}
				if uuid, existingQty, ok := bisleri.ExtractCartItem(cartHTML, productID); ok && uuid != "" {
					if existingQty == 0 {
						// Quantity parsing can be unreliable; accept presence of item after ensuring update request succeeds.
						if err := client.UpdateQuantity(ctx, productID, uuid, quantity); err != nil {
							lastErr = err
						} else {
							return nil
						}
					}
					if existingQty == quantity {
						return nil
					}
					if err := client.UpdateQuantity(ctx, productID, uuid, quantity); err != nil {
						lastErr = err
					} else {
						lastErr = fmt.Errorf("cart quantity was %d, updated to %d", existingQty, quantity)
					}
				} else if count, ok := bisleri.ExtractCartCount(cartHTML); ok && count == 0 {
					lastErr = errors.New("cart still empty")
				} else {
					lastErr = errors.New("product not yet visible in cart")
				}
			}
		}

		if attempt < maxAttempts {
			delay := time.Duration(attempt) * 500 * time.Millisecond
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}
	}
	if lastErr == nil {
		lastErr = errors.New("unknown cart verification error")
	}
	return fmt.Errorf("unable to confirm cart quantity after add: %v", lastErr)
}

func ensureCityLocation(ctx context.Context, client *bisleri.Client, profilePath string, profile *store.Profile, cartHTML string) (string, error) {
	selectedCity, ok := bisleri.ExtractSelectedCity(cartHTML)
	if ok && selectedCity != "" {
		return cartHTML, nil
	}
	options := bisleri.ExtractCityOptions(cartHTML)
	city := resolveCity(*profile, options)
	if city == "" {
		return cartHTML, nil
	}
	fmt.Println("Setting delivery city:", city)
	if err := client.SetCityLocation(ctx, city); err != nil {
		return cartHTML, err
	}
	profile.PreferredCity = city
	if err := store.SaveProfile(profilePath, *profile); err != nil {
		fmt.Fprintln(os.Stderr, "Warning: failed to save preferred city:", err)
	}
	refreshed, err := client.FetchCartPage(ctx)
	if err != nil {
		return cartHTML, err
	}
	return refreshed, nil
}

func tryCaptureAddress(profilePath string, profile *store.Profile) error {
	jar, err := bisleri.JarFromCookies(profile.Cookies)
	if err != nil {
		return err
	}
	client := bisleri.NewClient(&http.Client{Jar: jar, Timeout: 30 * time.Second}, log.New(os.Stderr, "", 0))
	ctx := context.Background()
	shippingHTML, err := client.FetchShippingPage(ctx)
	if err != nil {
		return err
	}
	candidates, err := bisleri.ParseAddressCandidates(shippingHTML)
	if err != nil || len(candidates) == 0 {
		return errors.New("no address candidates found")
	}
	var selected *bisleri.AddressCandidate
	for _, candidate := range candidates {
		if candidate.IsDefault {
			selected = &candidate
			break
		}
	}
	if selected == nil {
		selected = &candidates[0]
	}
	profile.AddressID = selected.ID
	profile.Address = &selected.Address
	profile.AddressSource = "shipping-page"
	return nil
}

func loadOrCreateProfile(name string) (store.Profile, string, error) {
	if name == "" {
		return store.Profile{}, "", errors.New("profile name required")
	}
	profilePath, err := config.ProfilePath(name)
	if err != nil {
		return store.Profile{}, "", err
	}
	profile, err := store.LoadProfile(profilePath)
	if err == nil {
		return profile, profilePath, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return store.Profile{}, "", err
	}
	if err := os.MkdirAll(filepath.Dir(profilePath), 0o700); err != nil {
		return store.Profile{}, "", err
	}
	profile = store.Profile{Name: name}
	if err := store.SaveProfile(profilePath, profile); err != nil {
		return store.Profile{}, "", err
	}
	return profile, profilePath, nil
}
