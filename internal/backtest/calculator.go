// Package backtest rejoue les stratégies de trading sur des bougies historiques.
//
// Il réutilise le VRAI code de décision (algorithms.ShouldBuy/ShouldSell) et la
// VRAIE math d'indicateurs (market.*Value), de sorte que le backtest reste fidèle
// au comportement de production tant que ce code évolue de façon cohérente.
package backtest

import (
	"fmt"
	"sort"
	"strconv"
	"time"

	"bot/internal/core/database"
	"bot/internal/market"
)

// Calculator implémente algorithms.IndicatorCalculator sur des bougies historiques.
//
// À chaque pas de la simulation, le moteur positionne le curseur `nowMs` ; les
// indicateurs ne voient alors que les bougies DÉJÀ CLÔTURÉES à cet instant
// (closeTime <= nowMs), pour éviter tout biais de look-ahead.
type Calculator struct {
	pair   string
	series map[string]timeframeSeries // par timeframe
	nowMs  int64
}

type timeframeSeries struct {
	candles    []database.Candle // ordre chronologique (plus ancienne d'abord)
	closeTimes []int64           // closeTime[i] = ouverture + durée de la bougie i
}

// NewCalculator construit un calculateur à partir des bougies par timeframe.
func NewCalculator(pair string, candlesByTF map[string][]database.Candle) *Calculator {
	series := make(map[string]timeframeSeries, len(candlesByTF))
	for tf, candles := range candlesByTF {
		sorted := make([]database.Candle, len(candles))
		copy(sorted, candles)
		sort.Slice(sorted, func(i, j int) bool { return sorted[i].Timestamp < sorted[j].Timestamp })
		dur := TimeframeMillis(tf)
		ct := make([]int64, len(sorted))
		for i, c := range sorted {
			ct[i] = c.Timestamp + dur
		}
		series[tf] = timeframeSeries{candles: sorted, closeTimes: ct}
	}
	return &Calculator{pair: pair, series: series}
}

// SetNow positionne le curseur temporel (« maintenant ») de la simulation.
func (c *Calculator) SetNow(ms int64) { c.nowMs = ms }

// closesAsOf renvoie les `window` dernières clôtures clôturées à c.nowMs.
func (c *Calculator) closesAsOf(timeframe string, window int) ([]float64, error) {
	s, ok := c.series[timeframe]
	if !ok || len(s.candles) == 0 {
		return nil, fmt.Errorf("backtest: aucune bougie pour %s/%s", c.pair, timeframe)
	}
	// Nombre de bougies dont la clôture est <= nowMs (recherche binaire).
	n := sort.Search(len(s.closeTimes), func(i int) bool { return s.closeTimes[i] > c.nowMs })
	if n == 0 {
		return nil, fmt.Errorf("backtest: pas encore de bougie %s clôturée à t=%d", timeframe, c.nowMs)
	}
	start := n - window
	if start < 0 {
		start = 0
	}
	closes := make([]float64, n-start)
	for i := start; i < n; i++ {
		closes[i-start] = s.candles[i].ClosePrice
	}
	return closes, nil
}

func (c *Calculator) CalculateRSI(pair, timeframe string, period int) (float64, error) {
	closes, err := c.closesAsOf(timeframe, market.RSIWindow(period))
	if err != nil {
		return 0, err
	}
	return market.RSIValue(closes, period)
}

func (c *Calculator) CalculateEMA(pair, timeframe string, period int) (float64, error) {
	closes, err := c.closesAsOf(timeframe, market.EMAWindow(period))
	if err != nil {
		return 0, err
	}
	return market.EMAValue(closes, period)
}

func (c *Calculator) CalculateVolatility(pair, timeframe string, period int) (float64, error) {
	closes, err := c.closesAsOf(timeframe, market.VolatilityWindow(period))
	if err != nil {
		return 0, err
	}
	return market.VolatilityValue(closes, period)
}

func (c *Calculator) CalculateMACD(pair, timeframe string, fastPeriod, slowPeriod, signalPeriod int) (float64, float64, float64, error) {
	closes, err := c.closesAsOf(timeframe, market.MACDWindow(slowPeriod))
	if err != nil {
		return 0, 0, 0, err
	}
	return market.MACDValue(closes, fastPeriod, slowPeriod, signalPeriod)
}

// CalculateRecentHigh renvoie le plus-haut sur les `periods` dernières bougies
// clôturées à c.nowMs (utilisé par la taille dynamique).
func (c *Calculator) CalculateRecentHigh(pair, timeframe string, periods int) (float64, error) {
	s, ok := c.series[timeframe]
	if !ok || len(s.candles) == 0 {
		return 0, fmt.Errorf("backtest: aucune bougie pour %s/%s", c.pair, timeframe)
	}
	n := sort.Search(len(s.closeTimes), func(i int) bool { return s.closeTimes[i] > c.nowMs })
	if n == 0 {
		return 0, fmt.Errorf("backtest: pas de bougie %s clôturée à t=%d", timeframe, c.nowMs)
	}
	start := n - periods
	if start < 0 {
		start = 0
	}
	high := s.candles[start].HighPrice
	for i := start; i < n; i++ {
		if s.candles[i].HighPrice > high {
			high = s.candles[i].HighPrice
		}
	}
	return high, nil
}

// TimeframeMillis convertit une timeframe ("1m","5m","1h","1d","1w","1M") en
// millisecondes. Réplique market.timeframeDuration (non exporté).
func TimeframeMillis(timeframe string) int64 {
	if len(timeframe) < 2 {
		return int64(time.Minute / time.Millisecond)
	}
	unit := timeframe[len(timeframe)-1]
	n, err := strconv.Atoi(timeframe[:len(timeframe)-1])
	if err != nil || n <= 0 {
		return int64(time.Minute / time.Millisecond)
	}
	var d time.Duration
	switch unit {
	case 'm':
		d = time.Duration(n) * time.Minute
	case 'h':
		d = time.Duration(n) * time.Hour
	case 'd':
		d = time.Duration(n) * 24 * time.Hour
	case 'w':
		d = time.Duration(n) * 7 * 24 * time.Hour
	case 'M':
		d = time.Duration(n) * 30 * 24 * time.Hour
	default:
		d = time.Minute
	}
	return int64(d / time.Millisecond)
}
