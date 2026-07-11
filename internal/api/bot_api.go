package api

import (
	"encoding/json"
	"net/http"
	"os"
	"sort"
	"strings"

	"bot/internal/logger"
)

type BotAPI struct {
	exchangeName string
	authToken    string
	bot          BotInterface
}

type BotInterface interface {
	ExchangeName() string
	ReloadStrategies() error
	CollectCandles(pair, timeframe string, limit int) (int, int, error) // fetched, saved, error
	// FetchBalances returns balances (asset → free/used/total amounts), base currency, quote currency, current price, error
	FetchBalances() (map[string]BalanceAmounts, string, string, float64, error)
	// ForceBuy déclenche un achat manuel immédiat et retourne un résumé de l'ordre posé.
	ForceBuy() (string, error)
}

// BalanceAmounts détaille un solde d'actif : disponible, bloqué en ordres ouverts, et total.
type BalanceAmounts struct {
	Free  float64
	Used  float64
	Total float64
}

type BalanceEntry struct {
	Asset string  `json:"asset"`
	Free  float64 `json:"free"`
	Used  float64 `json:"used"`
	Total float64 `json:"total"`
	Value float64 `json:"value"` // valorisation du total (free+used)
}

type BalanceResponse struct {
	Balances      []BalanceEntry `json:"balances"`
	TotalValue    float64        `json:"total_value"`
	QuoteCurrency string         `json:"quote_currency"`
}

func NewBotAPI(bot BotInterface) *BotAPI {
	token := os.Getenv("BOT_RELOAD_TOKEN")
	if token == "" {
		logger.Warnf("[%s] BOT_RELOAD_TOKEN not set, bot API will be disabled", bot.ExchangeName())
	}

	return &BotAPI{
		exchangeName: bot.ExchangeName(),
		authToken:    token,
		bot:          bot,
	}
}

func (api *BotAPI) Start() {
	if api.authToken == "" {
		logger.Infof("[%s] Bot API disabled (no token configured)", api.exchangeName)
		return
	}

	http.HandleFunc("/reload", api.handleReload)
	http.HandleFunc("/collect/candles", api.handleCollectCandles)
	http.HandleFunc("/health", api.handleHealth)
	http.HandleFunc("/balance", api.handleBalance)
	http.HandleFunc("/buy", api.handleBuy)

	port := os.Getenv("BOT_API_PORT")
	if port == "" {
		port = "9090"
	}

	logger.Infof("[%s] Starting bot API on port %s", api.exchangeName, port)
	go func() {
		if err := http.ListenAndServe(":"+port, nil); err != nil {
			logger.Errorf("Bot API server error: %v", err)
		}
	}()
}

func (api *BotAPI) handleReload(w http.ResponseWriter, r *http.Request) {
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

// handleBuy déclenche un achat manuel immédiat (override opérateur : hors condition
// RSI et hors cooldown). Sert au bouton Telegram « Acheter » et aux tests headless.
func (api *BotAPI) handleBuy(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	authHeader := r.Header.Get("Authorization")
	if !api.isValidToken(authHeader) {
		logger.Warnf("[%s] Invalid token for buy request from %s", api.exchangeName, r.RemoteAddr)
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
		return
	}

	logger.Infof("[%s] Manual buy request authenticated from %s", api.exchangeName, r.RemoteAddr)
	msg, err := api.bot.ForceBuy()

	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		logger.Errorf("[%s] Manual buy failed: %v", api.exchangeName, err)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "buy failed", "message": err.Error()})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "success", "message": msg})
}

func (api *BotAPI) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
}

func (api *BotAPI) isValidToken(authHeader string) bool {
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

func (api *BotAPI) handleCollectCandles(w http.ResponseWriter, r *http.Request) {
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

func (api *BotAPI) handleBalance(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	authHeader := r.Header.Get("Authorization")
	if !api.isValidToken(authHeader) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
		return
	}

	balances, baseAsset, quoteAsset, currentPrice, err := api.bot.FetchBalances()
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		logger.Errorf("[%s] Failed to fetch balances: %v", api.exchangeName, err)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	var entries []BalanceEntry
	totalValue := 0.0
	for asset, amounts := range balances {
		entry := BalanceEntry{Asset: asset, Free: amounts.Free, Used: amounts.Used, Total: amounts.Total}
		if asset == baseAsset && currentPrice > 0 {
			entry.Value = amounts.Total * currentPrice
		} else if asset == quoteAsset {
			entry.Value = amounts.Total
		}
		totalValue += entry.Value
		entries = append(entries, entry)
	}

	// Tri alphabétique pour un affichage stable
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Asset < entries[j].Asset
	})

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(BalanceResponse{
		Balances:      entries,
		TotalValue:    totalValue,
		QuoteCurrency: quoteAsset,
	})
}
