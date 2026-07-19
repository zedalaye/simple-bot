// Package botcli implémente la sous-commande « bot » : le daemon de trading.
package botcli

import (
	"bot/internal/api"
	"bot/internal/bot"
	"bot/internal/loader"
	"bot/internal/logger"
	"bot/internal/notify"
	"bot/internal/relay"
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

	tradingBot, cfg, err := loader.LoadBotWithConfig()
	if err != nil {
		log.Fatalf("Échec du chargement du bot : %v", err)
	}
	defer tradingBot.Cleanup()

	botAPI := api.NewBotAPI(tradingBot)
	botAPI.Start()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dashSource := bot.NewDashboardSource(tradingBot)

	// Canaux de notification. Multi les fait cohabiter : pendant la migration
	// vers le relay mobile, Telegram continue de recevoir les mêmes événements,
	// et la panne d'un canal n'en fait pas taire un autre.
	channels := notify.Multi{
		telegram.NewNotifier(tradingBot.Config.ExchangeName),
	}

	// Relay de notifications mobile (sortant uniquement, comme Telegram).
	relayCfg := relay.Config{
		URL:             cfg.RelayURL,
		Token:           cfg.RelayToken,
		Instance:        cfg.RelayInstance,
		Interval:        cfg.RelayInterval,
		BalanceInterval: cfg.RelayBalanceInterval,
	}
	if relayCfg.Enabled() {
		relayClient := relay.New(relayCfg, dashSource)
		relayClient.Start(ctx)
		channels = append(channels, relayClient)
		logger.Infof("✓ Relay mobile actif (%s, instance %s, snapshot %v)",
			relayCfg.URL, relayCfg.Instance, relayCfg.Interval)
	}

	tradingBot.SetNotifier(channels)

	// Dashboard Telegram interactif (long-polling sortant, pas d'exposition réseau).
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
