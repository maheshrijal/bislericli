package bisleri

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"bislericli/internal/store"
)

const (
	defaultBaseURL   = "https://www.bisleri.com"
	defaultUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/143.0.0.0 Safari/537.36"
)

var ErrNotAuthenticated = errors.New("session expired; please run 'bislericli auth login'")

type HTTPStatusError struct {
	Path       string
	Status     string
	StatusCode int
}

func (e *HTTPStatusError) Error() string {
	return fmt.Sprintf("%s request failed: %s", e.Path, e.Status)
}

type Client struct {
	BaseURL   string
	HTTP      *http.Client
	UserAgent string
	Logger    *log.Logger
	Throttle  time.Duration
	Debug     bool
}

func NewClient(httpClient *http.Client, logger *log.Logger) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}
	return &Client{
		BaseURL:   defaultBaseURL,
		HTTP:      httpClient,
		UserAgent: defaultUserAgent,
		Logger:    logger,
		Throttle:  900 * time.Millisecond,
		Debug:     false,
	}
}

func (c *Client) do(ctx context.Context, req *http.Request) (*http.Response, error) {
	c.applyHeaders(req)
	if c.Throttle > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(c.Throttle):
		}
	}
	c.logf("HTTP %s %s", req.Method, req.URL.String())
	return c.HTTP.Do(req.WithContext(ctx))
}

func (c *Client) newURL(path string) string {
	return strings.TrimRight(c.BaseURL, "/") + path
}

func (c *Client) applyHeaders(req *http.Request) {
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}
	if req.Header.Get("Accept") == "" {
		req.Header.Set("Accept", "*/*")
	}
	if req.Header.Get("Accept-Language") == "" {
		req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	}
}

func (c *Client) AddProduct(ctx context.Context, productID string, quantity int) error {
	if quantity <= 0 {
		return errors.New("quantity must be positive")
	}
	form := url.Values{}
	form.Set("pid", productID)
	form.Set("quantity", fmt.Sprintf("%d", quantity))
	form.Set("options", "[]")
	req, err := http.NewRequest("POST", c.newURL("/add-product"), strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")

	resp, err := c.do(ctx, req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("add product failed: %s", resp.Status)
	}
	if err := validateResponsePath(resp, ""); err != nil {
		return err
	}
	return nil
}

func (c *Client) UpdateJarQuantity(ctx context.Context, quantity int) error {
	if quantity < 0 {
		return errors.New("jar quantity cannot be negative")
	}
	path := fmt.Sprintf("/on/demandware.store/Sites-Bis-Site/default/Cart-UpdateJarQuantity?jarQuantity=%d", quantity)
	req, err := http.NewRequest("GET", c.newURL(path), nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	resp, err := c.do(ctx, req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("update jar quantity failed: %s", resp.Status)
	}
	if err := validateResponsePath(resp, ""); err != nil {
		return err
	}
	return nil
}

func (c *Client) UpdateQuantity(ctx context.Context, productID, uuid string, quantity int) error {
	path := fmt.Sprintf("/on/demandware.store/Sites-Bis-Site/default/Cart-UpdateQuantity?pid=%s&quantity=%d&uuid=%s", url.QueryEscape(productID), quantity, url.QueryEscape(uuid))
	req, err := http.NewRequest("GET", c.newURL(path), nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	resp, err := c.do(ctx, req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("update quantity failed: %s", resp.Status)
	}
	if err := validateResponsePath(resp, ""); err != nil {
		return err
	}
	return nil
}

func (c *Client) FetchShippingPage(ctx context.Context) (string, error) {
	return c.fetchPageWithRetry(ctx, "/checkout?stage=shipping", "/checkout")
}

func (c *Client) FetchPaymentPage(ctx context.Context) (string, error) {
	return c.fetchPageWithRetry(ctx, "/checkout?stage=payment", "/checkout")
}

func (c *Client) FetchCartPage(ctx context.Context) (string, error) {
	return c.fetchPageWithRetry(ctx, "/mycart", "/mycart")
}

func (c *Client) VerifyAuthenticated(ctx context.Context) error {
	body, resp, err := c.fetchPage(ctx, "/my-orders")
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("auth check failed: %s", resp.Status)
	}
	if err := validateResponsePath(resp, "/my-orders"); err != nil {
		return err
	}
	if len(body) == 0 {
		return errors.New("auth check failed: empty response")
	}
	return nil
}

func (c *Client) fetchPageWithRetry(ctx context.Context, path, expectedPrefix string) (string, error) {
	const maxAttempts = 3
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		body, resp, err := c.fetchPage(ctx, path)
		if err == nil && resp != nil {
			c.logf("Response %s %s", resp.Status, resp.Request.URL.String())
			if err := validateResponsePath(resp, expectedPrefix); err != nil {
				return "", err
			}
			if resp.StatusCode >= 400 {
				statusErr := &HTTPStatusError{Path: path, Status: resp.Status, StatusCode: resp.StatusCode}
				if resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests {
					lastErr = statusErr
					c.logf("Retrying %s after status %s (attempt %d/%d)", path, resp.Status, attempt, maxAttempts)
				} else {
					return "", statusErr
				}
			} else {
				return body, nil
			}
		} else if err != nil {
			lastErr = err
			c.logf("Request error for %s: %v", path, err)
		}

		if attempt < maxAttempts {
			delay := time.Duration(attempt) * time.Second
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(delay):
			}
		}
	}
	if lastErr == nil {
		lastErr = errors.New("unknown error")
	}
	return "", fmt.Errorf("request failed after retries: %w", lastErr)
}

func (c *Client) fetchPage(ctx context.Context, path string) (string, *http.Response, error) {
	url := c.newURL(path)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", nil, err
	}
	resp, err := c.do(ctx, req)
	if err != nil {
		return "", resp, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", resp, err
	}
	return string(body), resp, nil
}

// FetchPage is a public wrapper for fetchPage
func (c *Client) FetchPage(ctx context.Context, path string) (string, *http.Response, error) {
	return c.fetchPage(ctx, path)
}

func (c *Client) SubmitCheckoutForm(ctx context.Context, form CheckoutForm) error {
	action := strings.TrimSpace(form.Action)
	if action == "" {
		return errors.New("checkout form action missing")
	}
	if strings.HasPrefix(action, "/") {
		action = c.newURL(action)
	} else if !strings.HasPrefix(action, "http") {
		action = c.newURL("/" + action)
	}
	method := strings.ToUpper(strings.TrimSpace(form.Method))
	if method == "" {
		method = "POST"
	}
	var req *http.Request
	var err error
	switch method {
	case http.MethodGet:
		u, parseErr := url.Parse(action)
		if parseErr != nil {
			return parseErr
		}
		q := u.Query()
		for key, values := range form.Fields {
			for _, val := range values {
				q.Add(key, val)
			}
		}
		u.RawQuery = q.Encode()
		req, err = http.NewRequest(http.MethodGet, u.String(), nil)
	default:
		body := form.Fields.Encode()
		req, err = http.NewRequest(http.MethodPost, action, strings.NewReader(body))
		if err == nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
	}
	if err != nil {
		return err
	}
	if strings.HasPrefix(action, c.BaseURL) {
		req.Header.Set("Origin", c.BaseURL)
		req.Header.Set("Referer", c.newURL("/mycart"))
	}
	resp, err := c.do(ctx, req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return &HTTPStatusError{Path: action, Status: resp.Status, StatusCode: resp.StatusCode}
	}
	return nil
}

func (c *Client) BeginCheckout(ctx context.Context) error {
	cartHTML, err := c.FetchCartPage(ctx)
	if err != nil {
		return err
	}
	form, err := ExtractCheckoutForm(cartHTML)
	if err != nil {
		candidates := ExtractCheckoutCandidates(cartHTML)
		if c.Debug {
			c.logf("Checkout form not found. Candidates=%d", len(candidates))
			for i, candidate := range candidates {
				c.logf("Candidate %d: method=%s action=%s source=%s", i+1, candidate.Method, candidate.Action, candidate.Source)
			}
		}
		for _, candidate := range candidates {
			tryForm := CheckoutForm{
				Action: candidate.Action,
				Method: candidate.Method,
				Fields: url.Values{},
			}
			if submitErr := c.SubmitCheckoutForm(ctx, tryForm); submitErr == nil {
				return nil
			}
		}
		return err
	}
	if c.Debug {
		c.logf("Checkout form action=%s method=%s fields=%d has_csrf=%t",
			form.Action, form.Method, len(form.Fields), form.Fields.Get("csrf_token") != "")
	}
	return c.SubmitCheckoutForm(ctx, form)
}

func (c *Client) SetCityLocation(ctx context.Context, city string) error {
	city = strings.TrimSpace(city)
	if city == "" {
		return errors.New("city is required to set location")
	}
	form := url.Values{}
	form.Set("city", city)
	req, err := http.NewRequest("POST", c.newURL("/on/demandware.store/Sites-Bis-Site/default/LocationSelector-SetCityLocation"), strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	resp, err := c.do(ctx, req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return &HTTPStatusError{Path: "LocationSelector-SetCityLocation", Status: resp.Status, StatusCode: resp.StatusCode}
	}
	return nil
}

func (c *Client) SetSavedAddressLocation(ctx context.Context, address store.Address, addressID string) error {
	if addressID == "" {
		return errors.New("address ID is required to set saved address location")
	}
	form := url.Values{}
	form.Set("address1", address.Address1)
	form.Set("address2", address.Address2)
	form.Set("city", address.City)
	form.Set("stateCode", address.StateCode)
	form.Set("countryCode", address.Country)
	form.Set("postalCode", address.PostalCode)
	form.Set("addressID", addressID)
	fullName := strings.TrimSpace(strings.Join([]string{address.FirstName, address.LastName}, " "))
	form.Set("fullName", fullName)
	req, err := http.NewRequest("POST", c.newURL("/on/demandware.store/Sites-Bis-Site/default/LocationSelector-SetSavedAddressLocation"), strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	resp, err := c.do(ctx, req)
	if err != nil {
		return err
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode < 400 {
		return nil
	}
	if token := extractCSRFTokenFromJSON(body); token != "" {
		form.Set("csrf_token", token)
		retryReq, err := http.NewRequest("POST", c.newURL("/on/demandware.store/Sites-Bis-Site/default/LocationSelector-SetSavedAddressLocation"), strings.NewReader(form.Encode()))
		if err != nil {
			return err
		}
		retryReq.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
		retryReq.Header.Set("X-Requested-With", "XMLHttpRequest")
		retryResp, err := c.do(ctx, retryReq)
		if err != nil {
			return err
		}
		defer retryResp.Body.Close()
		if retryResp.StatusCode >= 400 {
			return &HTTPStatusError{Path: "LocationSelector-SetSavedAddressLocation", Status: retryResp.Status, StatusCode: retryResp.StatusCode}
		}
		return nil
	}
	return &HTTPStatusError{Path: "LocationSelector-SetSavedAddressLocation", Status: resp.Status, StatusCode: resp.StatusCode}
}

func extractCSRFTokenFromJSON(body []byte) string {
	type csrfPayload struct {
		CSRF struct {
			TokenName string `json:"tokenName"`
			Token     string `json:"token"`
		} `json:"csrf"`
	}
	var payload csrfPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	if payload.CSRF.Token != "" {
		return payload.CSRF.Token
	}
	return ""
}

func (c *Client) logf(format string, args ...interface{}) {
	if c == nil || !c.Debug || c.Logger == nil {
		return
	}
	c.Logger.Printf(format, args...)
}

func validateResponsePath(resp *http.Response, expectedPrefix string) error {
	if resp == nil || resp.Request == nil || resp.Request.URL == nil {
		return nil
	}
	path := resp.Request.URL.Path
	if expectedPrefix == "" || strings.HasPrefix(path, expectedPrefix) {
		return nil
	}
	lower := strings.ToLower(path)
	if path == "/" || path == "" {
		return ErrNotAuthenticated
	}
	if strings.Contains(lower, "login") || strings.Contains(lower, "account") || strings.Contains(lower, "home") {
		return ErrNotAuthenticated
	}
	return fmt.Errorf("unexpected redirect to %s", path)
}
