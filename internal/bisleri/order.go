package bisleri

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"bislericli/internal/store"
)

var orderIDRegex = regexp.MustCompile(`orderID=([^&]+)`) // matches query param

func (c *Client) SubmitShipping(ctx context.Context, shipmentUUID, csrfToken, timeslot string, address store.Address, addressID string) error {
	if shipmentUUID == "" || csrfToken == "" {
		return errors.New("missing shipment UUID or CSRF token")
	}
	form := url.Values{}
	form.Set("originalShipmentUUID", shipmentUUID)
	form.Set("shipmentUUID", shipmentUUID)
	if addressID != "" {
		form.Set("shipmentSelector", addressID)
	}
	form.Set("dwfrm_shipping_shippingAddress_addressFields_firstName", address.FirstName)
	form.Set("dwfrm_shipping_shippingAddress_addressFields_lastName", address.LastName)
	form.Set("dwfrm_shipping_shippingAddress_addressFields_floor", address.Floor)
	form.Set("dwfrm_shipping_shippingAddress_addressFields_address1", address.Address1)
	form.Set("dwfrm_shipping_shippingAddress_addressFields_address2", address.Address2)
	form.Set("dwfrm_shipping_shippingAddress_addressFields_nearByLandMark", address.NearByLandmark)
	form.Set("dwfrm_shipping_shippingAddress_addressFields_country", address.Country)
	form.Set("dwfrm_shipping_shippingAddress_addressFields_states_stateCode", address.StateCode)
	form.Set("dwfrm_shipping_shippingAddress_addressFields_city", address.City)
	form.Set("dwfrm_shipping_shippingAddress_addressFields_postalCode", address.PostalCode)
	form.Set("dwfrm_shipping_shippingAddress_addressFields_sector", "")
	form.Set("dwfrm_shipping_shippingAddress_addressFields_phone", address.Phone)
	form.Set("dwfrm_shipping_shippingAddress_shippingMethodID", "001")
	form.Set("dwfrm_shipping_shippingAddress_giftMessage", "")
	form.Set("csrf_token", csrfToken)
	if timeslot != "" {
		form.Set("timeslot", timeslot)
	}
	if address.Latitude != "" {
		form.Set("latitude", address.Latitude)
	}
	if address.Longitude != "" {
		form.Set("longitude", address.Longitude)
	}

	req, err := http.NewRequest("POST", c.newURL("/submit-shipping-address"), strings.NewReader(form.Encode()))
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
		return fmt.Errorf("submit shipping failed: %s", resp.Status)
	}
	_, _ = io.ReadAll(resp.Body)
	return nil
}

func (c *Client) SubmitPayment(ctx context.Context, shipmentUUID, csrfToken string, address store.Address) error {
	if shipmentUUID == "" || csrfToken == "" {
		return errors.New("missing shipment UUID or CSRF token")
	}
	form := url.Values{}
	form.Set("addressSelector", shipmentUUID)
	form.Set("dwfrm_billing_addressFields_firstName", address.FirstName)
	form.Set("dwfrm_billing_addressFields_lastName", address.LastName)
	form.Set("dwfrm_billing_addressFields_address1", address.Address1)
	form.Set("dwfrm_billing_addressFields_address2", address.Address2)
	form.Set("dwfrm_billing_addressFields_country", address.Country)
	form.Set("dwfrm_billing_addressFields_states_stateCode", address.StateCode)
	form.Set("dwfrm_billing_addressFields_city", address.City)
	form.Set("dwfrm_billing_addressFields_postalCode", address.PostalCode)
	form.Set("csrf_token", csrfToken)
	form.Set("localizedNewAddressTitle", "New Address")
	form.Set("dwfrm_billing_paymentMethod", "WALLET")

	req, err := http.NewRequest("POST", c.newURL("/on/demandware.store/Sites-Bis-Site/default/CheckoutServices-SubmitPayment"), strings.NewReader(form.Encode()))
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
		return fmt.Errorf("submit payment failed: %s", resp.Status)
	}
	_, _ = io.ReadAll(resp.Body)
	return nil
}

func (c *Client) PlaceOrder(ctx context.Context) (string, error) {
	client := *c.HTTP
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}

	req, err := http.NewRequest("GET", c.newURL("/on/demandware.store/Sites-Bis-Site/default/Wallet-WalletPlaceOrder"), nil)
	if err != nil {
		return "", err
	}
	c.applyHeaders(req)
	if c.Throttle > 0 {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(c.Throttle):
		}
	}
	resp, err := client.Do(req.WithContext(ctx))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound && resp.StatusCode != http.StatusSeeOther {
		return "", fmt.Errorf("place order failed: %s", resp.Status)
	}
	location := resp.Header.Get("Location")
	if location == "" {
		return "", errors.New("no redirect location from wallet place order")
	}
	if !strings.Contains(location, "/orderplaced") {
		return "", fmt.Errorf("unexpected redirect location: %s", location)
	}
	match := orderIDRegex.FindStringSubmatch(location)
	if len(match) > 1 {
		return match[1], nil
	}
	return "", nil
}
