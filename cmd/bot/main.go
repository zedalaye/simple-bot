package main

import (
	"bot/internal/api"
	"bot/internal/bot"
	"bot/internal/loader"
	"bot/internal/logger"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	projectRoot, err := os.Getwd()
	if err != nil {
		panic(err)
	}

	log.SetOutput(os.Stdout)
	log.Println("Starting Simple Bot")

	// Paramètres de ligne de commande
	var (
		botDir      = flag.String("root", ".", "Path to the bot directory")
		buyAtLaunch = flag.Bool("buy-at-launch", false, "Immediately place a buy order after startup")
	)
	flag.Parse()

	tradingBot, err := loader.LoadBot(projectRoot, *botDir)
	if err != nil {
		log.Fatalf("Failed to load bot: %v", err)
	}
	defer tradingBot.Cleanup()

	// Démarrer l'API de rechargement
	reloadAPI := api.NewReloadAPI(tradingBot)
	reloadAPI.Start()

	// Démarrer le bot
	err = tradingBot.Start(*buyAtLaunch)
	if err != nil {
		logger.Fatalf("Failed to start bot: %v", err)
	}

	// Gestion des signaux d'arrêt
	waitForShutdown(tradingBot)
}

func waitForShutdown(tradingBot *bot.Bot) {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	<-sigs
	logger.Infof("[%s] Got a stop signal. Stopping bot...", tradingBot.Config.ExchangeName)

	tradingBot.Stop()
	time.Sleep(1 * time.Second)

	tradingBot.ShowStatistics()
	logger.Infof("[%s] Simple Bot Stopped. See Ya!", tradingBot.Config.ExchangeName)
}
