package web

import (
	"log"
	"sort"
	"time"

	types "github.com/HORNET-Storage/hornet-storage/lib"
	"github.com/HORNET-Storage/hornet-storage/lib/stores/graviton"
	"github.com/gofiber/fiber/v2"
)

func handleBitcoinRatesForLast30Days(c *fiber.Ctx) error {
	// Initialize the Gorm database
	db, err := graviton.InitGorm()
	if err != nil {
		log.Printf("Failed to connect to the database: %v", err)
		return c.Status(fiber.StatusInternalServerError).SendString("Internal Server Error")
	}

	// Calculate the date 30 days ago
	thirtyDaysAgo := time.Now().AddDate(0, 0, -30)

	// Query the Bitcoin rates for the last 30 days
	var bitcoinRates []types.BitcoinRate
	result := db.Where("timestamp >= ?", thirtyDaysAgo).Order("timestamp asc").Find(&bitcoinRates)

	if result.Error != nil {
		log.Printf("Error querying Bitcoin rates: %v", result.Error)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Database query error",
		})
	}

	// Respond with the Bitcoin rates
	return c.JSON(bitcoinRates)
}

func handleBitcoinRatesForLast30DaysByCurrency(c *fiber.Ctx) error {
	// Initialize the Gorm database
	db, err := graviton.InitGorm()
	if err != nil {
		log.Printf("Failed to connect to the database: %v", err)
		return c.Status(fiber.StatusInternalServerError).SendString("Internal Server Error")
	}

	// Get the currency parameter from the route
	currency := c.Params("currency")
	if currency == "" {
		return c.Status(fiber.StatusBadRequest).SendString("Currency parameter is required")
	}

	// Validate the currency
	if !isValidCurrency(currency) {
		return c.Status(fiber.StatusBadRequest).SendString("Invalid currency")
	}

	// Calculate the date 30 days ago
	thirtyDaysAgo := time.Now().AddDate(0, 0, -30)

	// Query the Bitcoin rates for the last 30 days and the specified currency
	var bitcoinRates []types.BitcoinRate
	result := db.Where("timestamp >= ? AND currency = ?", thirtyDaysAgo, currency).
		Order("timestamp asc").
		Find(&bitcoinRates)

	if result.Error != nil {
		log.Printf("Error querying Bitcoin rates: %v", result.Error)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Database query error",
		})
	}

	// Check if any data exists for the last 30 days
	if len(bitcoinRates) < 3 {
		// No data exists, fetch historical prices from CoinGecko
		missingRates, err := fetchMissingHistoricalPrices(currency, nil)
		if err != nil {
			log.Printf("Error fetching historical prices: %v", err)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "Error fetching historical prices",
			})
		}

		// Save fetched historical prices to the database
		saveBitcoinRates(missingRates)

		// Combine the newly fetched rates
		bitcoinRates = append(bitcoinRates, missingRates...)
	}

	// Sort the rates by timestamp
	sort.Slice(bitcoinRates, func(i, j int) bool {
		return bitcoinRates[i].Timestamp.Before(bitcoinRates[j].Timestamp)
	})

	// Respond with the Bitcoin rates
	return c.JSON(bitcoinRates)
}

// isValidCurrency checks if the given currency is supported
func isValidCurrency(currency string) bool {
	supportedCurrencies := []string{"usd", "eur", "gbp", "jpy", "aud", "cad", "chf"}
	for _, c := range supportedCurrencies {
		if c == currency {
			return true
		}
	}
	return false
}
