package telegram

import (
	"log"
	"net/http"
	"net/url"
	"os"
)

var httpPostForm = http.PostForm

func SendMessage(message string) {
	if os.Getenv("TELEGRAM") != "1" {
		return
	}

	if os.Getenv("TELEGRAM_BOT_TOKEN") == "" {
		log.Fatal("Missing environment variable: TELEGRAM_BOT_TOKEN")
	}

	if os.Getenv("TELEGRAM_CHAT_ID") == "" {
		log.Fatal("Missing environment variable: TELEGRAM_CHAT_ID")
	}

	botToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	chatId := os.Getenv("TELEGRAM_CHAT_ID")

	endpoint := "https://api.telegram.org/bot" + botToken + "/sendMessage"

	data := url.Values{}
	data.Set("chat_id", chatId)
	data.Set("text", message)

	response, err := httpPostForm(endpoint, data)
	if err != nil {
		log.Println("Error sending Telegram message:", err)
	}

	if response.StatusCode != 200 {
		log.Println("Error sending Telegram message:", response.Status)
	}
}
