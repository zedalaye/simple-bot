// Commande patternscan : mesure le pouvoir prédictif des patterns de bougies de
// retournement haussier sur l'historique stocké en base. Pour chaque pattern, on
// rejoue les bougies « as-of » (sans regarder le futur), on détecte le signal, puis
// on mesure le rendement N bougies plus tard. On compare à une BASELINE (entrée sur
// chaque bougie) : un pattern n'a d'intérêt que s'il bat l'entrée au hasard.
// Package patternscancli implémente la sous-commande « patternscan » (voir en-tête ci-dessous).
package patternscancli

import (
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"bot/internal/loader"
	"bot/internal/market"
)

// Main est le point d'entrée de la sous-commande « patternscan ». La base analysée est
// celle de l'instance ciblée par le flag global --root (chdir géré en amont par le
// dispatcher), lue via loader.LoadOffline().
func Main(args []string) {
	var (
		pair        = flag.String("pair", "", "Paire à analyser (défaut : TRADING_PAIR de l'instance)")
		tf          = flag.String("tf", "4h", "Timeframe des bougies à analyser")
		horizonsStr = flag.String("horizons", "4,8,24", "Horizons forward en bougies (séparés par virgule)")
		declineBack = flag.Int("decline-lookback", 6, "Filtre contexte : n'évalue qu'après une baisse sur N bougies (0 = désactivé)")
		volMult     = flag.Float64("vol-mult", 0, "Filtre volume : exige volume ≥ mult × moyenne (0 = désactivé)")
		volLookback = flag.Int("vol-lookback", 20, "Fenêtre de moyenne du filtre volume")

		// Section « confirmations » : dissèque un pattern (marteau par défaut) avec divers
		// signaux de confirmation pour voir lesquels améliorent vraiment l'edge.
		confirmPattern = flag.String("pattern", "hammer", "Pattern à disséquer dans la section confirmations")
		rsiTh          = flag.Float64("rsi-th", 35, "Seuil RSI de la confirmation survente (RSI ≤ seuil)")
		rsiPeriod      = flag.Int("rsi-period", 14, "Période RSI")
		bbPeriod       = flag.Int("bb-period", 20, "Période Bollinger")
		bbK            = flag.Float64("bb-k", 2.0, "Nombre d'écarts-types Bollinger")
		divWindow      = flag.Int("div-window", 20, "Fenêtre de recherche de divergence RSI haussière")
	)
	flag.CommandLine.Parse(args)

	horizons := parseInts(*horizonsStr)
	if len(horizons) == 0 {
		log.Fatal("Aucun horizon valide")
	}
	maxH := 0
	for _, h := range horizons {
		if h > maxH {
			maxH = h
		}
	}

	cfg, db, err := loader.LoadOffline()
	if err != nil {
		log.Fatalf("Chargement de l'instance : %v", err)
	}
	defer db.Close()

	if *pair == "" {
		*pair = cfg.TradingPair
	}

	candles, err := db.GetAllCandles(*pair, *tf)
	if err != nil {
		log.Fatalf("Lecture bougies %s/%s : %v", *pair, *tf, err)
	}
	// Garantir l'ordre chronologique croissant.
	sort.Slice(candles, func(i, j int) bool { return candles[i].Timestamp < candles[j].Timestamp })
	if len(candles) < maxH+10 {
		log.Fatalf("Historique insuffisant pour %s/%s : %d bougies", *pair, *tf, len(candles))
	}
	oc := market.OHLCVFromCandles(candles)
	n := len(oc)

	from := time.UnixMilli(candles[0].Timestamp).UTC().Format("2006-01-02")
	to := time.UnixMilli(candles[n-1].Timestamp).UTC().Format("2006-01-02")

	// Accumulateurs : map pattern -> horizon -> liste des rendements forward.
	type acc struct {
		count int
		byH   map[int][]float64
	}
	stats := map[market.Pattern]*acc{}
	ensure := func(p market.Pattern) *acc {
		if stats[p] == nil {
			stats[p] = &acc{byH: map[int][]float64{}}
		}
		return stats[p]
	}
	baseline := &acc{byH: map[int][]float64{}}

	// Bougie i : on a besoin de l'historique [0..i] (as-of) et du futur jusqu'à i+maxH.
	for i := 2; i < n-maxH; i++ {
		series := oc[:i+1]

		// Baseline : rendements forward depuis CHAQUE bougie.
		baseline.count++
		for _, h := range horizons {
			baseline.byH[h] = append(baseline.byH[h], fwd(oc, i, h))
		}

		p := market.DetectBullishReversal(series)
		if p == market.PatternNone {
			continue
		}
		// Filtre de contexte : retournement après une baisse seulement.
		if *declineBack > 0 && !market.PrecededByDecline(series, *declineBack) {
			continue
		}
		// Filtre volume optionnel.
		if *volMult > 0 && !market.VolumeSpike(series, *volLookback, *volMult) {
			continue
		}

		a := ensure(p)
		a.count++
		for _, h := range horizons {
			a.byH[h] = append(a.byH[h], fwd(oc, i, h))
		}
	}

	// --- Affichage ---
	fmt.Printf("Pattern scan — %s %s  (%d bougies, %s → %s)\n", *pair, *tf, n, from, to)
	filters := []string{}
	if *declineBack > 0 {
		filters = append(filters, fmt.Sprintf("déclin>%d barres", *declineBack))
	}
	if *volMult > 0 {
		filters = append(filters, fmt.Sprintf("volume≥%.1f×moy(%d)", *volMult, *volLookback))
	}
	if len(filters) == 0 {
		filters = append(filters, "aucun")
	}
	fmt.Printf("Filtres : %s   |   Horizons (bougies) : %v\n\n", strings.Join(filters, ", "), horizons)

	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	header := "Pattern\tN"
	for _, h := range horizons {
		header += fmt.Sprintf("\t+%d %%win\t+%d moy%%\t+%d med%%", h, h, h)
	}
	fmt.Fprintln(w, header)

	printRow := func(name string, a *acc) {
		row := fmt.Sprintf("%s\t%d", name, a.count)
		for _, h := range horizons {
			rs := a.byH[h]
			row += fmt.Sprintf("\t%.0f\t%+.2f\t%+.2f", winRate(rs)*100, mean(rs)*100, median(rs)*100)
		}
		fmt.Fprintln(w, row)
	}

	printRow("BASELINE (toutes)", baseline)
	for _, p := range market.AllBullishPatterns {
		if a := stats[p]; a != nil && a.count > 0 {
			printRow(string(p), a)
		}
	}
	// Ligne agrégée « tous patterns confondus ».
	all := &acc{byH: map[int][]float64{}}
	for _, p := range market.AllBullishPatterns {
		if a := stats[p]; a != nil {
			all.count += a.count
			for _, h := range horizons {
				all.byH[h] = append(all.byH[h], a.byH[h]...)
			}
		}
	}
	if all.count > 0 {
		printRow("TOUS patterns", all)
	}
	w.Flush()

	fmt.Println("\nLecture : %win = part des cas où le prix est plus haut N bougies après ;")
	fmt.Println("moy%/med% = rendement moyen/médian à +N bougies. Un pattern est utile s'il")
	fmt.Println("bat nettement la ligne BASELINE (entrée sur n'importe quelle bougie).")

	// --- Section confirmations : dissèque un pattern avec divers signaux de confirmation. ---
	confirmationReport(oc, horizons, maxH, market.Pattern(*confirmPattern),
		*declineBack, *rsiPeriod, *rsiTh, *bbPeriod, *bbK, *divWindow, *volMult, *volLookback)
}

// confirmationReport compare un pattern « seul » à « pattern + chaque confirmation »
// (RSI survendu, volume, sous-Bollinger, divergence RSI, et la combo RSI+volume), pour
// voir laquelle améliore réellement le edge. Toutes les confirmations sont évaluées
// « as-of » (données jusqu'à la bougie de signal uniquement).
func confirmationReport(oc []market.OHLCV, horizons []int, maxH int, pat market.Pattern,
	declineBack, rsiPeriod int, rsiTh float64, bbPeriod int, bbK float64, divWindow int,
	volMult float64, volLookback int) {

	if _, ok := patternLabels[pat]; !ok {
		fmt.Printf("\n(Section confirmations ignorée : pattern inconnu %q)\n", pat)
		return
	}
	n := len(oc)

	closes := make([]float64, n)
	lows := make([]float64, n)
	for i := range oc {
		closes[i] = oc[i].Close
		lows[i] = oc[i].Low
	}

	// RSI précalculé une fois (aligné par la fin de série).
	rsiByIndex := make([]float64, n)
	for i := range rsiByIndex {
		rsiByIndex[i] = math.NaN()
	}
	if series, err := market.RSISeries(closes, rsiPeriod); err == nil {
		off := n - len(series)
		for k, v := range series {
			rsiByIndex[off+k] = v
		}
	}

	// Sommes préfixes pour Bollinger (moyenne + écart-type glissants sur bbPeriod).
	sum := make([]float64, n+1)
	sumSq := make([]float64, n+1)
	for i := 0; i < n; i++ {
		sum[i+1] = sum[i] + closes[i]
		sumSq[i+1] = sumSq[i] + closes[i]*closes[i]
	}
	lowerBB := func(i int) float64 {
		if i < bbPeriod-1 {
			return math.NaN()
		}
		start := i - bbPeriod + 1
		p := float64(bbPeriod)
		mean := (sum[i+1] - sum[start]) / p
		v := (sumSq[i+1]-sumSq[start])/p - mean*mean
		if v < 0 {
			v = 0
		}
		return mean - bbK*math.Sqrt(v)
	}

	// Divergence RSI haussière : sur les divWindow bougies précédentes, le prix fait un
	// plus-bas plus bas que le creux antérieur, mais le RSI est plus haut (force en hausse).
	bullDiv := func(i int) bool {
		lo := i - divWindow
		if lo < 1 {
			lo = 1
		}
		if lo >= i || math.IsNaN(rsiByIndex[i]) {
			return false
		}
		jmin := lo
		for j := lo; j < i; j++ {
			if lows[j] < lows[jmin] {
				jmin = j
			}
		}
		if math.IsNaN(rsiByIndex[jmin]) {
			return false
		}
		return lows[i] < lows[jmin] && rsiByIndex[i] > rsiByIndex[jmin]
	}

	vm := volMult
	if vm <= 0 {
		vm = 1.5
	}

	type confirm struct {
		name string
		pred func(i int) bool
	}
	confirms := []confirm{
		{fmt.Sprintf("%s (seul)", pat), func(i int) bool { return true }},
		{fmt.Sprintf("+ RSI≤%.0f", rsiTh), func(i int) bool { r := rsiByIndex[i]; return !math.IsNaN(r) && r <= rsiTh }},
		{fmt.Sprintf("+ volume≥%.1f×", vm), func(i int) bool { return market.VolumeSpike(oc[:i+1], volLookback, vm) }},
		{"+ sous Bollinger", func(i int) bool { lb := lowerBB(i); return !math.IsNaN(lb) && closes[i] < lb }},
		{"+ divergence RSI", bullDiv},
		{fmt.Sprintf("+ RSI≤%.0f & volume", rsiTh), func(i int) bool {
			r := rsiByIndex[i]
			return !math.IsNaN(r) && r <= rsiTh && market.VolumeSpike(oc[:i+1], volLookback, vm)
		}},
	}

	type acc struct {
		count int
		byH   map[int][]float64
	}
	buckets := make([]*acc, len(confirms))
	for k := range buckets {
		buckets[k] = &acc{byH: map[int][]float64{}}
	}
	base := &acc{byH: map[int][]float64{}} // baseline (toutes bougies) pour repère

	for i := 2; i < n-maxH; i++ {
		base.count++
		for _, h := range horizons {
			base.byH[h] = append(base.byH[h], fwd(oc, i, h))
		}

		if market.DetectBullishReversal(oc[:i+1]) != pat {
			continue
		}
		if declineBack > 0 && !market.PrecededByDecline(oc[:i+1], declineBack) {
			continue
		}
		for k, c := range confirms {
			if c.pred(i) {
				buckets[k].count++
				for _, h := range horizons {
					buckets[k].byH[h] = append(buckets[k].byH[h], fwd(oc, i, h))
				}
			}
		}
	}

	fmt.Printf("\n\n=== Confirmations pour %s (1h, après baisse) ===\n", pat)
	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	header := "Variante\tN"
	for _, h := range horizons {
		header += fmt.Sprintf("\t+%d %%win\t+%d moy%%\t+%d med%%", h, h, h)
	}
	fmt.Fprintln(w, header)

	printRow := func(name string, count int, byH map[int][]float64) {
		row := fmt.Sprintf("%s\t%d", name, count)
		for _, h := range horizons {
			rs := byH[h]
			row += fmt.Sprintf("\t%.0f\t%+.2f\t%+.2f", winRate(rs)*100, mean(rs)*100, median(rs)*100)
		}
		fmt.Fprintln(w, row)
	}

	printRow("BASELINE (toutes)", base.count, base.byH)
	for k, c := range confirms {
		printRow(c.name, buckets[k].count, buckets[k].byH)
	}
	w.Flush()
	fmt.Println("\nUne confirmation est utile si elle remonte nettement %win/rendement SANS")
	fmt.Println("écrouler N (sinon = sur-ajustement sur trop peu de cas).")
}

// patternLabels recense les patterns dissécables par la section confirmations.
var patternLabels = map[market.Pattern]bool{
	market.PatternHammer:           true,
	market.PatternBullishEngulfing: true,
	market.PatternPiercingLine:     true,
	market.PatternMorningStar:      true,
}

// fwd renvoie le rendement de la clôture i à la clôture i+h.
func fwd(oc []market.OHLCV, i, h int) float64 {
	return oc[i+h].Close/oc[i].Close - 1
}

func winRate(rs []float64) float64 {
	if len(rs) == 0 {
		return 0
	}
	win := 0
	for _, r := range rs {
		if r > 0 {
			win++
		}
	}
	return float64(win) / float64(len(rs))
}

func mean(rs []float64) float64 {
	if len(rs) == 0 {
		return 0
	}
	var s float64
	for _, r := range rs {
		s += r
	}
	return s / float64(len(rs))
}

func median(rs []float64) float64 {
	if len(rs) == 0 {
		return 0
	}
	cp := append([]float64(nil), rs...)
	sort.Float64s(cp)
	m := len(cp) / 2
	if len(cp)%2 == 1 {
		return cp[m]
	}
	return (cp[m-1] + cp[m]) / 2
}

func parseInts(s string) []int {
	var out []int
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if v, err := strconv.Atoi(p); err == nil && v > 0 {
			out = append(out, v)
		}
	}
	return out
}
