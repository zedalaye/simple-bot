package contract

import (
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
)

// Le contrat est partagé entre le bot et le relay. Le bot tire ccxt et SQLite ;
// le relay ne doit tirer ni l'un ni l'autre, sous peine de voir son image passer
// de quelques Mo à plus de cent.
//
// Il suffit de vérifier les imports *directs* : si tous relèvent de la
// bibliothèque standard, la fermeture transitive l'est aussi.
func TestContractDependsOnlyOnStdlib(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("lecture du paquet impossible : %v", err)
	}

	fset := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue // les tests peuvent importer ce qu'ils veulent
		}

		file, err := parser.ParseFile(fset, name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("%s : analyse impossible : %v", name, err)
		}

		for _, imp := range file.Imports {
			dep, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				t.Fatalf("%s : import illisible %s", name, imp.Path.Value)
			}
			if isStdlib(dep) {
				continue
			}
			t.Errorf("%s importe %q : le contrat doit rester sans dépendance "+
				"(sinon le binaire relay tire les dépendances du bot)", name, dep)
		}
	}
}

// isStdlib : un chemin de la bibliothèque standard n'a pas de point dans son
// premier segment, contrairement à « github.com/... » ou « bot/internal/... »
// (ce dernier étant rattrapé par le préfixe du module).
func isStdlib(path string) bool {
	if strings.HasPrefix(path, "bot/") {
		return false
	}
	first, _, _ := strings.Cut(path, "/")
	return !strings.Contains(first, ".")
}
