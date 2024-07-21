package web

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"

	types "github.com/HORNET-Storage/hornet-storage/lib"
	"github.com/HORNET-Storage/hornet-storage/lib/stores/graviton"
	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

type CoinGeckoResponse struct {
	Bitcoin struct {
		USD float64 `json:"usd"`
	} `json:"bitcoin"`
}
type CoinGeckoFiatResponse struct {
	Bitcoin map[string]float64 `json:"bitcoin"`
}

type BinanceResponse struct {
	Price string `json:"price"`
}

// TODO: Merge the two CoinGecko fetching functions into one
func fetchCoinGeckoPrice() (float64, error) {
	resp, err := http.Get("https://api.coingecko.com/api/v3/simple/price?ids=bitcoin&vs_currencies=usd")
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}

	var result CoinGeckoResponse
	err = json.Unmarshal(body, &result)
	if err != nil {
		return 0, err
	}

	return result.Bitcoin.USD, nil
}
func fetchCoinPriceByFiat(fiat string) (float64, error) {
	url := fmt.Sprintf("https://api.coingecko.com/api/v3/simple/price?ids=bitcoin&vs_currencies=%s", fiat)
	resp, err := http.Get(url)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}

	var result CoinGeckoFiatResponse
	err = json.Unmarshal(body, &result)
	if err != nil {
		return 0, err
	}

	price, exists := result.Bitcoin[fiat]
	if !exists {
		return 0, fmt.Errorf("fiat currency %s not found in response", fiat)
	}
	fmt.Println("Price of Bitcoin in", fiat, ":", price)

	return price, nil
}
func handleBitcoinPriceByCurrency(c *fiber.Ctx) error {
	// Get the currency parameter from the route
	currency := c.Params("currency")
	if currency == "" {
		return c.Status(fiber.StatusBadRequest).SendString("Currency parameter is required")
	}

	// Validate the currency
	if !isValidCurrency(currency) {
		return c.Status(fiber.StatusBadRequest).SendString("Invalid currency")
	}

	price, err := fetchCoinPriceByFiat(currency)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	return c.JSON(fiber.Map{
		"price": price,
	})
}
func fetchBinancePrice() (float64, error) {
	resp, err := http.Get("https://api.binance.com/api/v3/ticker/price?symbol=BTCUSDT")
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}

	var result BinanceResponse
	err = json.Unmarshal(body, &result)
	if err != nil {
		return 0, err
	}

	price, err := strconv.ParseFloat(result.Price, 64)
	if err != nil {
		return 0, err
	}

	return price, nil
}

func fetchBitcoinPrice(apiIndex int) (float64, int, error) {
	apis := []func() (float64, error){
		fetchCoinGeckoPrice,
		fetchBinancePrice,
	}

	for i := 0; i < len(apis); i++ {
		index := (apiIndex + i) % len(apis)
		price, err := apis[index]()
		if err == nil {
			return price, (index + 1) % len(apis), nil
		}
		fmt.Println("Error fetching price from API", index, ":", err)
	}

	return 0, apiIndex, fmt.Errorf("all API calls failed")
}

func pullBitcoinPrice() {
	// Fetch the initial Bitcoin rate immediately
	apiIndex := 0
	price, newIndex, err := fetchBitcoinPrice(apiIndex)
	if err != nil {
		fmt.Println("Error:", err)
	} else {
		fmt.Printf("Initial Bitcoin Price from APIs: $%.2f\n", price)
		apiIndex = newIndex
		saveBitcoinRate(price)
	}

	// Set up the ticker for subsequent fetches
	ticker := time.NewTicker(7 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		price, newIndex, err := fetchBitcoinPrice(apiIndex)
		if err != nil {
			fmt.Println("Error:", err)
		} else {
			fmt.Printf("Bitcoin Price from APIs: $%.2f\n", price)
			apiIndex = newIndex
			saveBitcoinRate(price)
		}
	}
}

func saveBitcoinRate(rate float64) {
	// Initialize the Gorm database
	db, err := graviton.InitGorm()
	if err != nil {
		log.Printf("Failed to connect to the database: %v", err)
		return
	}

	// Query the latest Bitcoin rate
	var latestBitcoinRate types.BitcoinRate
	result := db.Order("timestamp desc").First(&latestBitcoinRate)

	if result.Error != nil && result.Error != gorm.ErrRecordNotFound {
		log.Printf("Error querying bitcoin rate: %v", result.Error)
		return
	}

	if result.Error == nil && latestBitcoinRate.Rate == rate {
		// If the rate is the same as the latest entry, no update needed
		fmt.Println("Rate is the same as the latest entry, no update needed")
		return
	}

	// Add the new rate
	newRate := types.BitcoinRate{
		Rate:      rate,
		Timestamp: time.Now(),
	}
	if err := db.Create(&newRate).Error; err != nil {
		log.Printf("Error saving new rate: %v", err)
		return
	}

	fmt.Println("Bitcoin rate updated successfully")
}
