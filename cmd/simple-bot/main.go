// Commande simple-bot : binaire unique regroupant toutes les sous-commandes du projet
// (bot, web, admin, backtest, patternscan, order, rsi, volatility, test). Le code commun
// n'est ainsi compilé et déployé qu'une seule fois. Chaque sous-commande vit dans un
// package internal/cli/<nom>cli exposant Main(args []string).
//
// Usage : simple-bot [--root DIR] <commande> [options de la commande...]
//
// --root est un flag GLOBAL, géré ici en amont : il doit précéder le nom de la commande.
// Le dispatcher capture le cwd d'origine (templates/ + static/ de la WebUI) puis applique
// le chdir une seule fois, avant de déléguer. Les sous-commandes n'ont donc plus à gérer
// --root ni le changement de répertoire.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"

	"bot/internal/cli"
	"bot/internal/cli/admincli"
	"bot/internal/cli/backtestcli"
	"bot/internal/cli/botcli"
	"bot/internal/cli/ordercli"
	"bot/internal/cli/patternscancli"
	"bot/internal/cli/rsicli"
	"bot/internal/cli/testcli"
	"bot/internal/cli/volatilitycli"
	"bot/internal/cli/webcli"
	"bot/internal/version"
)

// commands associe le nom de chaque sous-commande à son point d'entrée.
var commands = map[string]func(args []string){
	"bot":         botcli.Main,
	"web":         webcli.Main,
	"admin":       admincli.Main,
	"backtest":    backtestcli.Main,
	"patternscan": patternscancli.Main,
	"order":       ordercli.Main,
	"rsi":         rsicli.Main,
	"volatility":  volatilitycli.Main,
	"test":        testcli.Main,
	"version":     func(_ []string) { printVersion() },
}

func printVersion() {
	fmt.Printf("simple-bot %s\n", version.Version)
}

func main() {
	log.SetOutput(os.Stdout)

	// FlagSet dédié pour les flags globaux : on ne touche PAS flag.CommandLine, que les
	// sous-commandes réutilisent pour parser leurs propres options via Main(args).
	fs := flag.NewFlagSet("simple-bot", flag.ExitOnError)
	root := fs.String("root", ".", "Répertoire racine de l'instance du bot")
	showVersion := fs.Bool("version", false, "Affiche la version puis quitte")
	fs.Usage = usage
	// Parse s'arrête au premier argument non-flag : le nom de la sous-commande.
	_ = fs.Parse(os.Args[1:])

	// --version est global : il court-circuite le dispatch (comme `version`).
	if *showVersion {
		printVersion()
		return
	}

	rest := fs.Args()
	if len(rest) == 0 {
		usage()
		os.Exit(2)
	}
	name, cmdArgs := rest[0], rest[1:]

	cmd, ok := commands[name]
	if !ok {
		fmt.Fprintf(os.Stderr, "Commande inconnue : %s\n\n", name)
		usage()
		os.Exit(2)
	}

	// Capturer le cwd d'origine (templates/ + static/ pour la WebUI) AVANT le chdir,
	// puis appliquer --root une seule fois, en amont de toutes les sous-commandes.
	originWd, err := os.Getwd()
	if err != nil {
		log.Fatalf("Impossible de déterminer le répertoire courant : %v", err)
	}
	cli.RootWd = originWd

	if *root != "." {
		if err := os.Chdir(*root); err != nil {
			log.Fatalf("Impossible de changer de répertoire vers %s : %v", *root, err)
		}
	}

	cmd(cmdArgs)
}

func usage() {
	names := make([]string, 0, len(commands))
	for n := range commands {
		names = append(names, n)
	}
	sort.Strings(names)

	fmt.Fprintf(os.Stderr, "Simple Bot %s\n\n", version.Version)
	fmt.Fprintf(os.Stderr, "Usage : simple-bot [--root DIR] <commande> [options]\n\n")
	fmt.Fprintf(os.Stderr, "Commandes disponibles : %s\n", strings.Join(names, ", "))
}
