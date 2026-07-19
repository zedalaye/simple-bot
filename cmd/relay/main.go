// Commande relay : le service que le bot alimente et que l'application mobile
// consulte.
//
// Il se déploie sur un hôte public, à l'inverse du bot qui reste sur le réseau
// domestique. Il ne connaît aucune clé d'exchange et ne touche pas à la base du
// bot : il ne manipule que ce que le bot lui pousse.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"bot/internal/logger"
	"bot/internal/relayserver"
	"bot/internal/version"
)

func main() {
	log.Printf("Starting Simple Bot Relay %s", version.Version)

	if err := logger.InitLogger(getenv("LOG_LEVEL", "info"), os.Getenv("LOG_FILE")); err != nil {
		log.Fatalf("Échec de l'initialisation du logger : %v", err)
	}

	cfg := relayserver.Config{
		Addr:        getenv("RELAY_ADDR", ":9000"),
		IngestToken: os.Getenv("RELAY_INGEST_TOKEN"),
		APIToken:    os.Getenv("RELAY_API_TOKEN"),
		Silence:     time.Duration(getenvInt("RELAY_SILENCE_MINUTES", 5)) * time.Minute,
	}

	// Sans jeton, la surface correspondante refuse tout. Autant le dire au
	// démarrage plutôt que de laisser chercher pourquoi le bot prend des 401.
	if cfg.IngestToken == "" {
		logger.Error("RELAY_INGEST_TOKEN non défini : l'ingestion refusera toutes les requêtes")
	}
	if cfg.APIToken == "" {
		logger.Error("RELAY_API_TOKEN non défini : l'API mobile refusera toutes les requêtes")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv := relayserver.New(cfg, relayserver.NewStore())
	srv.StartWatchdog(ctx)

	httpServer := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}

	go func() {
		logger.Infof("✓ Relay à l'écoute sur %s (silence toléré : %v)", cfg.Addr, cfg.Silence)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Fatalf("Échec de l'écoute : %v", err)
		}
	}()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	<-sigs

	logger.Info("Signal d'arrêt reçu. Arrêt du relay...")
	cancel()

	shutdownCtx, stop := context.WithTimeout(context.Background(), 10*time.Second)
	defer stop()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Errorf("Arrêt non gracieux : %v", err)
	}
	logger.Info("Relay arrêté. À bientôt !")
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getenvInt(key string, fallback int) int {
	v, err := strconv.Atoi(os.Getenv(key))
	if err != nil || v <= 0 {
		return fallback
	}
	return v
}
