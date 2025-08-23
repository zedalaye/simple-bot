package telegram

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
)

var httpPostForm = http.PostForm

func SendMessage(message string) error {
	if os.Getenv("TELEGRAM") != "1" {
		return nil
	}

	if os.Getenv("TELEGRAM_BOT_TOKEN") == "" {
		return fmt.Errorf("missing environment variable: TELEGRAM_BOT_TOKEN")
	}

	if os.Getenv("TELEGRAM_CHAT_ID") == "" {
		return fmt.Errorf("missing environment variable: TELEGRAM_CHAT_ID")
	}

	botToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	chatId := os.Getenv("TELEGRAM_CHAT_ID")

	endpoint := "https://api.telegram.org/bot" + botToken + "/sendMessage"

	data := url.Values{}
	data.Set("chat_id", chatId)
	data.Set("text", message)

	response, err := httpPostForm(endpoint, data)
	if err != nil {
		return err
	}

	if response.StatusCode != 200 {
		return fmt.Errorf("failed to send Telegram message (Status=%v)", response.Status)
	}

	return nil
}
