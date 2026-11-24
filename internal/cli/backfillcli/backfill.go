// Commande backfill : rapatrie l'historique de bougies de la paire depuis l'exchange
// et l'enregistre dans la base de l'instance. Le collecteur du bot ne récupère que
// les dernières bougies ; cette commande comble le passé, typiquement au démarrage
// d'une nouvelle instance (indicateurs prêts tout de suite, backtests sur les prix
// réels de l'exchange).
//
// Exemple : simple-bot --root storage/okx backfill
//
//	simple-bot --root storage/okx backfill --tf 15m,1h --since 2026-01-01
//
// Idempotent : les bougies déjà présentes sont réécrites avec les valeurs de l'exchange
// (upsert), on peut relancer la commande sans risque. Sert aussi à RÉPARER une plage
// (bougies incomplètes, trous) : --since <date> la recharge entièrement.
package backfillcli

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"bot/internal/backtest"
	"bot/internal/bot"
	"bot/internal/core/database"
	"bot/internal/exchange"
	"bot/internal/loader"
)

// pageSize : nombre de bougies par requête (maximum accepté par OKX).
const pageSize = 300

// Main est le point d'entrée de la sous-commande « backfill ». La base alimentée est
// celle de l'instance ciblée par le flag global --root.
func Main(args []string) {
	log.SetOutput(os.Stderr)

	var (
		pair     = flag.String("pair", "", "Paire à rapatrier (défaut : TRADING_PAIR de l'instance)")
		tfsStr   = flag.String("tf", "15m,1h,4h,1d,1w,1M", "Timeframes à rapatrier, séparées par des virgules")
		sinceStr = flag.String("since", "", "Début (YYYY-MM-DD) ; vide = depuis la cotation de la paire")
	)
	flag.CommandLine.Parse(args)

	cfg, db, err := loader.LoadOffline()
	if err != nil {
		log.Fatalf("Chargement de l'instance : %v", err)
	}
	defer db.Close()

	if *pair == "" {
		*pair = cfg.TradingPair
	}

	// Données publiques : aucune clé API nécessaire.
	ex := exchange.NewExchange(cfg.ExchangeName)
	if ex == nil {
		log.Fatalf("Exchange non supporté : %q", cfg.ExchangeName)
	}

	start, err := listingStart(ex, *pair)
	if err != nil {
		log.Fatalf("Date de cotation de %s introuvable : %v", *pair, err)
	}
	fmt.Printf("%s %s : historique disponible depuis %s\n", cfg.ExchangeName, *pair, day(start))
	if *sinceStr != "" {
		t, err := time.Parse("2006-01-02", *sinceStr)
		if err != nil {
			log.Fatalf("--since invalide (attendu YYYY-MM-DD) : %v", err)
		}
		if ms := t.UnixMilli(); ms > start {
			start = ms
		}
	}

	for _, tf := range strings.Split(*tfsStr, ",") {
		tf = strings.TrimSpace(tf)
		if tf == "" {
			continue
		}
		n, first, last, err := backfill(ex, db, *pair, tf, start)
		if err != nil {
			log.Fatalf("%s : %v (relancer la commande reprend là où elle s'est arrêtée)", tf, err)
		}
		if n == 0 {
			fmt.Printf("  %-4s aucune bougie\n", tf)
			continue
		}
		fmt.Printf("  %-4s %6d bougies, %s → %s\n", tf, n, day(first), day(last))
	}
}

// listingStart renvoie l'ouverture de la première bougie mensuelle de la paire : une
// seule requête couvre 300 mois, soit toute l'histoire possible d'une paire crypto.
// Sert de plancher pour ne pas paginer des années de fenêtres vides.
func listingStart(ex *exchange.Exchange, pair string) (int64, error) {
	since := time.Date(2010, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli()
	candles, err := ex.FetchCandles(pair, "1M", &since, pageSize)
	if err != nil {
		return 0, err
	}
	if len(candles) == 0 {
		return 0, fmt.Errorf("aucune bougie mensuelle")
	}
	return candles[0].Timestamp, nil
}

// backfill pagine de start jusqu'à maintenant et enregistre les bougies CLÔTURÉES (la
// bougie en cours est ignorée : le collecteur du bot la tient à jour jusqu'à sa clôture).
// Renvoie le nombre de bougies enregistrées et leurs bornes.
func backfill(ex *exchange.Exchange, db *database.DB, pair, tf string, start int64) (n int, first, last int64, err error) {
	dur := backtest.TimeframeMillis(tf)
	now := time.Now().UnixMilli()
	since := start

	for since < now {
		var page []bot.Candle
		page, err = ex.FetchCandles(pair, tf, &since, pageSize)
		if err != nil {
			return n, first, last, err
		}
		if len(page) == 0 {
			// Fenêtre vide (avant la cotation, ou trou de données) : on saute d'une page.
			since += pageSize * dur
			continue
		}

		for _, c := range page {
			if c.Timestamp+dur > now {
				continue // bougie en cours
			}
			if err = db.SaveCandle(pair, tf, c.Timestamp, c.Open, c.High, c.Low, c.Close, c.Volume); err != nil {
				return n, first, last, err
			}
			if n == 0 {
				first = c.Timestamp
			}
			last = c.Timestamp
			n++
		}

		next := page[len(page)-1].Timestamp + 1
		if next <= since {
			break // pas de progression : l'exchange renvoie toujours la même page
		}
		since = next
	}
	return n, first, last, nil
}

func day(ms int64) string {
	return time.UnixMilli(ms).UTC().Format("2006-01-02 15:04")
}
