package web

import (
	"log"
	"strconv"

	types "github.com/HORNET-Storage/hornet-storage/lib"
	"github.com/HORNET-Storage/hornet-storage/lib/stores/graviton"
	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

func handleBalanceByCurrency(c *fiber.Ctx) error {
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

	// Get the latest wallet balance
	var latestBalance types.WalletBalance
	result := db.Order("timestamp desc").First(&latestBalance)

	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			log.Printf("No wallet balance found, using default value")
			latestBalance.Balance = "0" // Set default balance
		} else {
			log.Printf("Error querying latest balance: %v", result.Error)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "Database query error",
			})
		}
	}

	// Get the latest Bitcoin rate for the specified currency
	var bitcoinRate types.BitcoinRate
	result = db.Where("currency = ?", currency).Order("timestamp desc").First(&bitcoinRate)

	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			log.Printf("No Bitcoin rate found for currency %s, using default value", currency)
			bitcoinRate.Rate = 0.0 // Set default rate
		} else {
			log.Printf("Error querying Bitcoin rate for currency %s: %v", currency, result.Error)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "Database query error",
			})
		}
	}

	// Convert the balance to the specified currency
	satoshis, err := strconv.ParseInt(latestBalance.Balance, 10, 64)
	if err != nil {
		log.Printf("Error converting balance to int64: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Conversion error",
		})
	}

	currencyBalance := satoshiToUSD(bitcoinRate.Rate, satoshis)

	// Respond with the balance in the specified currency
	return c.JSON(fiber.Map{
		"balance_currency": currencyBalance,
		"latest_balance":   satoshis,
	})
}
