// Package cli regroupe l'infrastructure partagée par les sous-commandes du binaire
// unique simple-bot (voir cmd/simple-bot). Chaque sous-commande vit dans un
// sous-package internal/cli/<nom>cli exposant une fonction Main(args []string).
package cli

// RootWd est le répertoire de travail d'origine, capturé par le dispatcher AVANT
// l'application du flag global --root (chdir). La WebUI y localise templates/ et
// static/, qui restent relatifs à la racine du dépôt et non à l'instance.
var RootWd string
