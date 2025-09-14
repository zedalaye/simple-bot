package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"bot/internal/logger"
)

type BotClient struct {
	baseURL   string
	authToken string
	client    *http.Client
}

type BotResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Error   string `json:"error"`
}

type CollectCandlesRequest struct {
	Pair      string `json:"pair"`
	Timeframe string `json:"timeframe"`
	Since     *int64 `json:"since,omitempty"`
	Limit     int    `json:"limit,omitempty"`
}

type CollectCandlesResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Error   string `json:"error"`
	Fetched int    `json:"fetched,omitempty"`
	Saved   int    `json:"saved,omitempty"`
}

func NewBotClient() *BotClient {
	baseURL := os.Getenv("BOT_API_URL")
	if baseURL == "" {
		baseURL = "http://bot:9090" // Default pour Docker
	}

	token := os.Getenv("BOT_RELOAD_TOKEN")
	if token == "" {
		logger.Warnf("BOT_RELOAD_TOKEN not set, bot notifications disabled")
	}

	return &BotClient{
		baseURL:   baseURL,
		authToken: token,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (bc *BotClient) NotifyReload() error {
	if bc.authToken == "" {
		logger.Debugf("Bot reload token not configured, skipping notification")
		return nil
	}

	url := fmt.Sprintf("%s/reload", bc.baseURL)

	req, err := http.NewRequest("POST", url, bytes.NewBuffer([]byte("{}")))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+bc.authToken)

	logger.Debugf("Sending reload notification to bot at %s", url)

	resp, err := bc.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send reload request: %w", err)
	}
	defer resp.Body.Close()

	var botResponse BotResponse
	if err := json.NewDecoder(resp.Body).Decode(&botResponse); err != nil {
		// Si on ne peut pas décoder, on continue avec un message générique
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("bot reload failed with status code: %d", resp.StatusCode)
		}
	}

	if resp.StatusCode != http.StatusOK {
		errorMsg := botResponse.Error
		if errorMsg == "" {
			errorMsg = fmt.Sprintf("HTTP %d", resp.StatusCode)
		}
		return fmt.Errorf("bot reload failed: %s", errorMsg)
	}

	logger.Infof("Bot strategies reload notification sent successfully")
	return nil
}

// RequestCandleCollection demande au bot de collecter des bougies pour une paire/timeframe
func (bc *BotClient) RequestCandleCollection(pair, timeframe string, since *int64, limit int) (*CollectCandlesResponse, error) {
	if bc.authToken == "" {
		return nil, fmt.Errorf("bot reload token not configured")
	}

	url := fmt.Sprintf("%s/collect/candles", bc.baseURL)

	request := CollectCandlesRequest{
		Pair:      pair,
		Timeframe: timeframe,
		Since:     since,
		Limit:     limit,
	}

	requestBody, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(requestBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+bc.authToken)

	logger.Debugf("Requesting candle collection from bot: %s %s (limit: %d)", pair, timeframe, limit)

	resp, err := bc.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send collection request: %w", err)
	}
	defer resp.Body.Close()

	var collectResponse CollectCandlesResponse
	if err := json.NewDecoder(resp.Body).Decode(&collectResponse); err != nil {
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("candle collection failed with status code: %d", resp.StatusCode)
		}
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		errorMsg := collectResponse.Error
		if errorMsg == "" {
			errorMsg = fmt.Sprintf("HTTP %d", resp.StatusCode)
		}
		return nil, fmt.Errorf("candle collection failed: %s", errorMsg)
	}

	logger.Infof("Bot candle collection completed: fetched %d, saved %d new candles for %s %s",
		collectResponse.Fetched, collectResponse.Saved, pair, timeframe)
	return &collectResponse, nil
}

// CheckHealth vérifie si le bot API est accessible
func (bc *BotClient) CheckHealth() error {
	if bc.authToken == "" {
		return fmt.Errorf("bot reload token not configured")
	}

	url := fmt.Sprintf("%s/health", bc.baseURL)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create health check request: %w", err)
	}

	resp, err := bc.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send health check request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bot health check failed with status code: %d", resp.StatusCode)
	}

	return nil
}
