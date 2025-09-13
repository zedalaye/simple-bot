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
