package web

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	types "github.com/HORNET-Storage/hornet-storage/lib"
	"github.com/HORNET-Storage/hornet-storage/lib/stores/graviton"
	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

var supportedCurrencies = []string{"usd", "eur", "gbp", "jpy", "aud", "cad", "chf"}

type CoinGeckoFiatResponse struct {
	Bitcoin map[string]float64 `json:"bitcoin"`
}

type BinanceResponse struct {
	Price string `json:"price"`
}

type MempoolResponse struct {
	Time int64   `json:"time"`
	USD  float64 `json:"USD"`
	EUR  float64 `json:"EUR"`
	GBP  float64 `json:"GBP"`
	CAD  float64 `json:"CAD"`
	CHF  float64 `json:"CHF"`
	AUD  float64 `json:"AUD"`
	JPY  float64 `json:"JPY"`
}

// / API CALLS
func fetchCoinGeckoPrices(fiats []string) (map[string]float64, error) {
	fiatList := strings.Join(fiats, ",")
	url := fmt.Sprintf("https://api.coingecko.com/api/v3/simple/price?ids=bitcoin&vs_currencies=%s", fiatList)
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result CoinGeckoFiatResponse
	err = json.Unmarshal(body, &result)
	if err != nil {
		return nil, err
	}

	if result.Bitcoin == nil {
		return nil, fmt.Errorf("no data found in CoinGecko response")
	}

	return result.Bitcoin, nil
}
func fetchMempoolPrice(fiats []string) (map[string]float64, error) {
	resp, err := http.Get("https://mempool.space/api/v1/prices")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var result MempoolResponse
	err = json.Unmarshal(body, &result)
	if err != nil {
		return nil, err
	}
	prices := make(map[string]float64)
	for _, fiat := range fiats {
		switch fiat {
		case "usd":
			prices["usd"] = result.USD
		case "eur":
			prices["eur"] = result.EUR
		case "gbp":
			prices["gbp"] = result.GBP
		case "cad":
			prices["cad"] = result.CAD
		case "chf":
			prices["chf"] = result.CHF
		case "aud":
			prices["aud"] = result.AUD
		case "jpy":
			prices["jpy"] = result.JPY
		default:
			// If a fiat currency is not in the response, you might want to handle it
			// e.g., prices[fiat] = 0 or return an error
		}
	}

	return prices, nil

}
func fetchBinancePrice() (map[string]float64, error) {
	resp, err := http.Get("https://api.binance.com/api/v3/ticker/price?symbol=BTCUSDT")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result BinanceResponse
	err = json.Unmarshal(body, &result)
	if err != nil {
		return nil, err
	}

	price, err := strconv.ParseFloat(result.Price, 64)
	if err != nil {
		return nil, err
	}

	// Return the price in a map with the key "usd"
	return map[string]float64{
		"usd": price,
	}, nil
}

/// API CALLS ^^

func fetchBitcoinPrice(apiIndex int, fiats []string) (map[string]float64, int, error) {
	apis := []func(fiats []string) (map[string]float64, error){
		fetchCoinGeckoPrices,
		//	fetchBinancePrice,
		fetchMempoolPrice,
	}
	for i := 0; i < len(apis); i++ {
		index := (apiIndex + i) % len(apis)
		price, err := apis[index](fiats)
		if err == nil {
			return price, (index + 1) % len(apis), nil
		}
	}

	return nil, apiIndex, fmt.Errorf("all API calls failed")

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

	price, err := fetchCoinGeckoPrices([]string{currency})
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	return c.JSON(fiber.Map{
		"price": price,
	})
}

func pullBitcoinPrices() {
	// Define supported currencies

	// Fetch the initial Bitcoin rates immediately
	apiIndex := 0
	prices, newIndex, err := fetchBitcoinPrice(apiIndex, supportedCurrencies)
	if err != nil {
		fmt.Println("Error fetching initial prices:", err)
	} else {

		for currency, price := range prices {
			fmt.Printf("Initial Bitcoin Price in %s: $%.2f\n", currency, price)
			apiIndex = newIndex
			saveBitcoinRate(currency, price)
		}
	} // Set up the ticker for subsequent fetches
	ticker := time.NewTicker(7 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		prices, newIndex, err := fetchBitcoinPrice(apiIndex, supportedCurrencies)
		if err != nil {
			fmt.Println("Error fetching prices:", err)
		} else {
			for currency, price := range prices {
				fmt.Printf("Bitcoin Price in %s: $%.2f\n", currency, price)
				apiIndex = newIndex
				saveBitcoinRate(currency, price)
			}
		}
	}
}

func saveBitcoinRate(currency string, rate float64) {
	db, err := graviton.InitGorm()
	if err != nil {
		log.Printf("Failed to connect to the database: %v", err)
		return
	}

	var latestBitcoinRate types.BitcoinRate
	result := db.Where("currency = ?", currency).Order("timestamp desc").First(&latestBitcoinRate)

	if result.Error != nil && result.Error != gorm.ErrRecordNotFound {
		log.Printf("Error querying bitcoin rate: %v", result.Error)
		return
	}

	if result.Error == nil && latestBitcoinRate.Rate == rate {
		fmt.Println("Rate is the same as the latest entry, no update needed")
		return
	}

	newRate := types.BitcoinRate{
		Rate:      rate,
		Currency:  currency,
		Timestamp: time.Now(),
	}
	if err := db.Create(&newRate).Error; err != nil {
		log.Printf("Error saving new rate: %v", err)
		return
	}

	fmt.Println("Bitcoin rate updated successfully for currency", currency)
}
func saveBitcoinRates(rates []types.BitcoinRate) {
	db, err := graviton.InitGorm()
	if err != nil {
		log.Printf("Failed to connect to the database: %v", err)
		return
	}

	for _, rate := range rates {
		if err := db.Create(&rate).Error; err != nil {
			log.Printf("Error saving rate for %s on %s: %v", rate.Currency, rate.Timestamp.Format("2006-01-02"), err)
		} else {
		}
	}
}

type CoinGeckoHistoricalResponse struct {
	Prices [][]float64 `json:"prices"`
}

// fetchMissingHistoricalPrices fetches the historical prices from CoinGecko that are not in the existingDates map
func fetchMissingHistoricalPrices(fiat string, existingDates map[string]bool) ([]types.BitcoinRate, error) {
	url := fmt.Sprintf("https://api.coingecko.com/api/v3/coins/bitcoin/market_chart?vs_currency=%s&days=30", fiat)
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result CoinGeckoHistoricalResponse
	err = json.Unmarshal(body, &result)
	if err != nil {
		return nil, err
	}

	if result.Prices == nil {
		return nil, fmt.Errorf("no data found in CoinGecko response")
	}

	var missingRates []types.BitcoinRate
	for _, priceData := range result.Prices {
		timestamp := time.Unix(int64(priceData[0])/1000, 0)
		dateStr := timestamp.Format("2006-01-02")
		if !existingDates[dateStr] {
			missingRates = append(missingRates, types.BitcoinRate{
				Rate:      priceData[1],
				Currency:  fiat,
				Timestamp: timestamp,
			})
		}
	}

	return missingRates, nil
}
