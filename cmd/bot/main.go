package main

import (
	"bot/internal/api"
	"bot/internal/bot"
	"bot/internal/loader"
	"bot/internal/logger"
	"bot/internal/version"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	log.SetOutput(os.Stdout)
	log.Printf("Starting Simple Bot %s", version.Version)

	var (
		botDir      = flag.String("root", ".", "Répertoire racine de l'instance du bot")
		buyAtLaunch = flag.Bool("buy-at-launch", false, "Immédiatement placer un ordre d'achat au démarrage")
	)
	flag.Parse()

	if *botDir != "." {
		if err := os.Chdir(*botDir); err != nil {
			log.Fatalf("Impossible de changer de répertoire vers %s : %v", *botDir, err)
		}
	}

	tradingBot, err := loader.LoadBot()
	if err != nil {
		log.Fatalf("Échec du chargement du bot : %v", err)
	}
	defer tradingBot.Cleanup()

	botAPI := api.NewBotAPI(tradingBot)
	botAPI.Start()

	err = tradingBot.Start(*buyAtLaunch)
	if err != nil {
		logger.Fatalf("Échec du démarrage du bot : %v", err)
	}

	waitForShutdown(tradingBot)
}

func waitForShutdown(tradingBot *bot.Bot) {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	<-sigs
	logger.Infof("[%s] Signal d'arrêt reçu. Arrêt du bot...", tradingBot.Config.ExchangeName)

	tradingBot.Stop()
	time.Sleep(1 * time.Second)

	tradingBot.ShowStatistics()
	logger.Infof("[%s] Simple Bot arrêté. À bientôt !", tradingBot.Config.ExchangeName)
}
