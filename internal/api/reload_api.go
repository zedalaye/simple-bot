package api

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"

	"bot/internal/logger"
)

type ReloadAPI struct {
	exchangeName string
	authToken    string
	bot          BotInterface
}

type BotInterface interface {
	ExchangeName() string
	ReloadStrategies() error
	CollectCandles(pair, timeframe string, limit int) (int, int, error) // fetched, saved, error
}

func NewReloadAPI(bot BotInterface) *ReloadAPI {
	token := os.Getenv("BOT_RELOAD_TOKEN")
	if token == "" {
		logger.Warnf("[%s] BOT_RELOAD_TOKEN not set, reload API will be disabled", bot.ExchangeName())
	}

	return &ReloadAPI{
		exchangeName: bot.ExchangeName(),
		authToken:    token,
		bot:          bot,
	}
}

func (api *ReloadAPI) Start() {
	if api.authToken == "" {
		logger.Infof("[%s] Reload API disabled (no token configured)", api.exchangeName)
		return
	}

	http.HandleFunc("/reload", api.handleReload)
	http.HandleFunc("/collect/candles", api.handleCollectCandles)
	http.HandleFunc("/health", api.handleHealth)

	port := os.Getenv("BOT_API_PORT")
	if port == "" {
		port = "9090"
	}

	logger.Infof("[%s] Starting reload API on port %s", api.exchangeName, port)
	go func() {
		if err := http.ListenAndServe(":"+port, nil); err != nil {
			logger.Errorf("Reload API server error: %v", err)
		}
	}()
}

func (api *ReloadAPI) handleReload(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	// Vérifier l'authentification
	authHeader := r.Header.Get("Authorization")
	if !api.isValidToken(authHeader) {
		logger.Warnf("[%s] Invalid token for reload request from %s", api.exchangeName, r.RemoteAddr)
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
		return
	}

	// Exécuter le rechargement
	logger.Infof("[%s] Reload request authenticated from %s", api.exchangeName, r.RemoteAddr)
	err := api.bot.ReloadStrategies()

	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		logger.Errorf("Failed to reload strategies: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error":   "reload failed",
			"message": err.Error(),
		})
		return
	}

	logger.Infof("[%s] Strategies reloaded successfully", api.exchangeName)
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "success",
		"message": "strategies reloaded",
	})
}

func (api *ReloadAPI) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
}

func (api *ReloadAPI) isValidToken(authHeader string) bool {
	if api.authToken == "" || authHeader == "" {
		return false
	}

	// Format: "Bearer <token>"
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || parts[0] != "Bearer" {
		return false
	}

	return parts[1] == api.authToken
}

type CollectCandlesRequest struct {
	Pair      string `json:"pair"`
	Timeframe string `json:"timeframe"`
	Limit     int    `json:"limit,omitempty"`
}

func (api *ReloadAPI) handleCollectCandles(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	// Vérifier l'authentification
	authHeader := r.Header.Get("Authorization")
	if !api.isValidToken(authHeader) {
		logger.Warnf("[%s] Invalid token for collect candles request from %s", api.exchangeName, r.RemoteAddr)
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
		return
	}

	// Décoder la requête
	var req CollectCandlesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logger.Errorf("[%s] Invalid collect candles request: %v", api.exchangeName, err)
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid request"})
		return
	}

	if req.Pair == "" || req.Timeframe == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "pair and timeframe are required"})
		return
	}

	if req.Limit <= 0 {
		req.Limit = 500 // valeur par défaut
	}

	// Exécuter la collecte
	logger.Infof("[%s] Collect candles request authenticated from %s for %s %s (limit: %d)",
		api.exchangeName, r.RemoteAddr, req.Pair, req.Timeframe, req.Limit)

	fetched, saved, err := api.bot.CollectCandles(req.Pair, req.Timeframe, req.Limit)

	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		logger.Errorf("Failed to collect candles: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error":   "collection failed",
			"message": err.Error(),
			"status":  "error",
		})
		return
	}

	logger.Infof("[%s] Candles collected successfully: fetched %d, saved %d", api.exchangeName, fetched, saved)
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "success",
		"message": "candles collected successfully",
		"fetched": fetched,
		"saved":   saved,
	})
}
