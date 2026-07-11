package bot

import (
	"fmt"
	"strings"
	"time"

	"bot/internal/core/database"
	"bot/internal/logger"
	"bot/internal/market"
	"bot/internal/telegram"
)

// Le moniteur de retournement surveille les BOUGIES 1h et notifie sur Telegram quand
// un signal de creux (marteau / étoile du matin après une baisse) se forme. C'est de
// la LECTURE SEULE : aucune action de trading n'est déclenchée — l'opérateur décide
// (ex. bouton « 🛒 Acheter »). Le choix du 1h et des deux patterns vient de l'analyse
// la commande patternscan : c'est le seul timeframe où ces formes battent une entrée au hasard
// sur BTC ; l'avalement/perçante sont trop fréquents/faibles → exclus pour ne pas spammer.
const (
	patternTimeframe        = "1h"
	patternTimeframeMs      = 3600 * 1000
	patternDeclineLookback  = 6  // baisse préalable exigée (en bougies)
	patternVolumeLookback   = 20 // fenêtre moyenne pour la confirmation volume
	patternVolumeMultiplier = 1.5
	patternRSIOversold      = 35 // seuil RSI(1h) de survente (cf. la commande patternscan : booste l'edge du marteau)
)

// convictionTag classe la force d'un signal selon le nombre de confirmations réunies
// (RSI survendu, volume au-dessus de la normale). L'analyse patternscan montre que
// le marteau passe de ~58 % de réussite à ~68 % (RSI), ~72 % (volume) et ~75 % (les deux).
func convictionTag(rsiOversold, volConfirmed bool) string {
	switch {
	case rsiOversold && volConfirmed:
		return "🔥 haute conviction"
	case rsiOversold || volConfirmed:
		return "⭐ conviction moyenne"
	default:
		return ""
	}
}

// notifiablePatterns : patterns notifiés + leur libellé d'affichage.
var notifiablePatterns = map[market.Pattern]string{
	market.PatternHammer:      "Marteau 🔨",
	market.PatternMorningStar: "Étoile du matin 🌅",
}

// evaluateReversalSignal décide, à partir des bougies 1h (ordre chronologique) et de
// l'instant courant, s'il y a un signal de retournement à notifier. Logique PURE et
// testable : sélection de la dernière bougie clôturée, garde-fou de fraîcheur, détection
// du pattern, filtre de baisse préalable et filtre des patterns notifiables.
// Renvoie l'index de la bougie clôturée (-1 si indéterminé) et ok=true si à notifier.
func evaluateReversalSignal(candles []database.Candle, now time.Time) (p market.Pattern, closedIdx int, ok bool) {
	if len(candles) < patternDeclineLookback+5 {
		return market.PatternNone, -1, false
	}

	// Dernière bougie CLÔTURÉE : si la plus récente est encore en formation, prendre la précédente.
	closedIdx = len(candles) - 1
	if now.UnixMilli() < candles[closedIdx].Timestamp+patternTimeframeMs {
		closedIdx--
	}
	if closedIdx < patternDeclineLookback+2 {
		return market.PatternNone, -1, false
	}

	// Garde-fou « fraîcheur » : ne traiter que la bougie qui vient de clôturer (évite une
	// notif périmée au démarrage ou après une longue absence de collecte).
	closedTs := candles[closedIdx].Timestamp
	if now.UnixMilli()-(closedTs+patternTimeframeMs) > patternTimeframeMs {
		return market.PatternNone, closedIdx, false
	}

	series := market.OHLCVFromCandles(candles[:closedIdx+1])
	p = market.DetectBullishReversal(series)
	if _, notifiable := notifiablePatterns[p]; !notifiable {
		return p, closedIdx, false
	}
	if !market.PrecededByDecline(series, patternDeclineLookback) {
		return p, closedIdx, false
	}
	return p, closedIdx, true
}

// checkReversalSignal évalue la dernière bougie 1h clôturée et envoie une notif Telegram
// si un pattern de creux s'y forme. Dédup par bougie : un signal donné n'est notifié qu'une fois.
func (b *Bot) checkReversalSignal() {
	pair := b.Config.Pair

	// Rafraîchir les bougies 1h (best effort ; on continue avec la base si l'API échoue).
	if err := b.marketCollector.CollectCandles(pair, patternTimeframe, 60); err != nil {
		logger.Debugf("[%s] Moniteur patterns : collecte 1h échouée : %v", b.Config.ExchangeName, err)
	}

	candles, err := b.db.GetCandles(pair, patternTimeframe, 60) // ordre chronologique (ASC)
	if err != nil {
		return
	}

	p, closedIdx, ok := evaluateReversalSignal(candles, time.Now())
	if !ok {
		return
	}

	// Dédup : ne notifier qu'une fois par bougie (Swap renvoie l'ancienne valeur).
	closedTs := candles[closedIdx].Timestamp
	if b.lastPatternCandleTs.Swap(closedTs) == closedTs {
		return
	}

	// Confirmations : enrichissent le message et déterminent le niveau de conviction
	// (sans incidence sur le déclenchement, qui reste « marteau/étoile après baisse »).
	series := market.OHLCVFromCandles(candles[:closedIdx+1])
	closed := candles[closedIdx]

	volConfirmed := market.VolumeSpike(series, patternVolumeLookback, patternVolumeMultiplier)
	rsiVal, rsiErr := b.Calculator.CalculateRSI(pair, patternTimeframe, 14)
	rsiOversold := rsiErr == nil && rsiVal <= patternRSIOversold

	header := string(notifiablePatterns[p])
	if tag := convictionTag(rsiOversold, volConfirmed); tag != "" {
		header += "  ·  " + tag
	}

	details := []string{}
	if rsiErr == nil {
		s := fmt.Sprintf("RSI(1h) %.0f", rsiVal)
		if rsiOversold {
			s += " (survendu)"
		}
		details = append(details, s)
	}
	if volConfirmed {
		details = append(details, "volume confirmé ✅")
	}
	detailLine := ""
	if len(details) > 0 {
		detailLine = "\n" + strings.Join(details, "  ·  ")
	}

	msg := fmt.Sprintf(
		"🔔 [%s] Signal de retournement haussier — %s (1h)\n%s%s\nClôture : %s %s\nCreux possible. /status ou bouton 🛒 Acheter pour agir.",
		b.Config.ExchangeName, pair,
		header, detailLine,
		b.market.FormatPrice(closed.ClosePrice), b.market.QuoteAsset,
	)
	if err := telegram.SendMessage(msg); err != nil {
		logger.Errorf("[%s] Échec notif pattern Telegram : %v", b.Config.ExchangeName, err)
		return
	}
	logger.Infof("[%s] Signal de retournement notifié (%s) sur bougie 1h %s", b.Config.ExchangeName,
		notifiablePatterns[p], time.UnixMilli(closedTs).UTC().Format("2006-01-02 15:04"))
}
