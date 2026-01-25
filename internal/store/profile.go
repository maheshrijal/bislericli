package store

import (
	"encoding/json"
	"errors"
	"os"
	"time"
)

type Cookie struct {
	Name     string `json:"name"`
	Value    string `json:"value"`
	Domain   string `json:"domain"`
	Path     string `json:"path"`
	Expires  int64  `json:"expires"`
	Secure   bool   `json:"secure"`
	HTTPOnly bool   `json:"httpOnly"`
	SameSite string `json:"sameSite"`
}

type Address struct {
	FirstName      string `json:"firstName"`
	LastName       string `json:"lastName"`
	Floor          string `json:"floor"`
	Address1       string `json:"address1"`
	Address2       string `json:"address2"`
	NearByLandmark string `json:"nearByLandmark"`
	City           string `json:"city"`
	StateCode      string `json:"stateCode"`
	PostalCode     string `json:"postalCode"`
	Country        string `json:"country"`
	Phone          string `json:"phone"`
	Latitude       string `json:"latitude"`
	Longitude      string `json:"longitude"`
}

type OrderInfo struct {
	OrderID    string    `json:"orderId"`
	PlacedAt   time.Time `json:"placedAt"`
	TotalPrice string    `json:"totalPrice"`
}

type Profile struct {
	Name          string     `json:"name"`
	Cookies       []Cookie   `json:"cookies"`
	AddressID     string     `json:"addressId"`
	Address       *Address   `json:"address,omitempty"`
	PreferredCity string     `json:"preferredCity,omitempty"`
	PhoneNumber   string     `json:"phoneNumber,omitempty"`
	LastLogin     time.Time  `json:"lastLogin"`
	LastOrder     *OrderInfo `json:"lastOrder,omitempty"`
	AddressSource string     `json:"addressSource,omitempty"`
}

func LoadProfile(path string) (Profile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Profile{}, err
	}
	var profile Profile
	if err := json.Unmarshal(data, &profile); err != nil {
		return Profile{}, err
	}
	if profile.Name == "" {
		return Profile{}, errors.New("profile is missing name")
	}
	return profile, nil
}

func SaveProfile(path string, profile Profile) error {
	data, err := json.MarshalIndent(profile, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
