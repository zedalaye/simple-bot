package premium

import (
	"bot/internal/logger"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
)

// Me, because I paid in BTC and thus my customer id cannot be verified against Stripe subscriptions.
const WHITELISTED = "bc8a2144196e807dceb366a593ba963f89be9dd69592a0680e1c98e7fc841e6d"

func CheckPremiumness() error {
	var customerId string = os.Getenv("CUSTOMER_ID")

	if customerId == "" {
		return errors.New("you need to set CUSTOMER_ID in .env or through environment variables")
	}

	h := sha256.New()
	h.Write([]byte(customerId))
	bs := fmt.Sprintf("%x", h.Sum(nil))

	if WHITELISTED == bs {
		logger.Info("Premium check is ok. CustomerId is Whitelisted")
		return nil
	}

	url := "https://validator.cryptomancien.com"
	body, err := json.Marshal(
		map[string]string{
			"CUSTOMER_ID": os.Getenv("CUSTOMER_ID"),
		},
	)
	if err != nil {
		logger.Error("CheckPremiumness() failed to marshall json")
		return err
	}

	resp, err := http.Post(url, "application/json", bytes.NewBuffer(body))
	if err != nil {
		logger.Error("CheckPremiumness() failed to post request")
		return err
	}

	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			logger.Error(err)
		}
	}(resp.Body)

	statusCode := resp.StatusCode
	if statusCode != 200 {
		logger.Error("CheckPremiumness() subscription has expired.")
		return errors.New("subscription has expired, go to https://cryptomancien.com and update your subscription")
	}

	logger.Info("Premium Subscription is valid")
	return nil
}
