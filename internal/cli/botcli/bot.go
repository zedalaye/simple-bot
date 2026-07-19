// Package botcli implémente la sous-commande « bot » : le daemon de trading.
package botcli

import (
	"bot/internal/api"
	"bot/internal/bot"
	"bot/internal/loader"
	"bot/internal/logger"
	"bot/internal/notify"
	"bot/internal/telegram"
	"bot/internal/version"
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// Main est le point d'entrée de la sous-commande « bot ». Le flag --root et le chdir
// sont gérés en amont par le dispatcher (cmd/simple-bot).
func Main(args []string) {
	log.Printf("Starting Simple Bot %s", version.Version)

	buyAtLaunch := flag.Bool("buy-at-launch", false, "Immédiatement placer un ordre d'achat au démarrage")
	flag.CommandLine.Parse(args)

	tradingBot, err := loader.LoadBot()
	if err != nil {
		log.Fatalf("Échec du chargement du bot : %v", err)
	}
	defer tradingBot.Cleanup()

	botAPI := api.NewBotAPI(tradingBot)
	botAPI.Start()

	// Canaux de notification. Multi permet de faire tourner plusieurs canaux en
	// parallèle (Telegram + relay mobile) pendant une migration.
	tradingBot.SetNotifier(notify.Multi{
		telegram.NewNotifier(tradingBot.Config.ExchangeName),
	})

	// Dashboard Telegram interactif (long-polling sortant, pas d'exposition réseau).
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dashSource := bot.NewDashboardSource(tradingBot)
	tgDashboard := telegram.StartPolling(ctx, dashSource)

	err = tradingBot.Start(*buyAtLaunch)
	if err != nil {
		logger.Fatalf("Échec du démarrage du bot : %v", err)
	}

	waitForShutdown(tradingBot, tgDashboard)
	cancel()
}

func waitForShutdown(tradingBot *bot.Bot, tgDashboard *telegram.Handle) {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	<-sigs
	logger.Infof("[%s] Signal d'arrêt reçu. Arrêt du bot...", tradingBot.Config.ExchangeName)

	tradingBot.Stop()
	time.Sleep(1 * time.Second)

	// Transforme le dashboard Telegram en bannière « arrêté » (arrêt propre).
	tgDashboard.NotifyStopped()

	tradingBot.ShowStatistics()
	logger.Infof("[%s] Simple Bot arrêté. À bientôt !", tradingBot.Config.ExchangeName)
}
