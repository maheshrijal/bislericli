package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"bislericli/internal/config"
)

// Order represents a saved order, mirroring bisleri.Order but independent for storage
type SavedOrder struct {
	OrderID   string  `json:"orderId"`
	Date      string  `json:"date"`      // String representation
	ParsedDate time.Time `json:"parsedDate"` // Parsed for sorting
	Status    string  `json:"status"`
	Total     string  `json:"total"`     // "₹200"
	Amount    float64 `json:"amount"`    // 200.00
	Items     string  `json:"items"`
}

type OrderHistory struct {
	LastSynced time.Time    `json:"lastSynced"`
	Orders     []SavedOrder `json:"orders"`
}

func GetOrdersPath(profileName string) (string, error) {
	// Use standard config directory
	configDir, err := config.ConfigDir()
	if err != nil {
		return "", err
	}
	
	// Store in "data" subdirectory
	dir := filepath.Join(configDir, "data")
	
	// Secure permissions (0700)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	return filepath.Join(dir, "orders_"+profileName+".json"), nil
}

func SaveOrderHistory(profileName string, orders []SavedOrder) error {
	path, err := GetOrdersPath(profileName)
	if err != nil {
		return err
	}

	history := OrderHistory{
		LastSynced: time.Now(),
		Orders:     orders,
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(history)
}

func LoadOrderHistory(profileName string) (*OrderHistory, error) {
	path, err := GetOrdersPath(profileName)
	if err != nil {
		return nil, err
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var history OrderHistory
	if err := json.NewDecoder(f).Decode(&history); err != nil {
		return nil, err
	}
	return &history, nil
}
