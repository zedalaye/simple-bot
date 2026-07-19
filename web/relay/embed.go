// Package relayui embarque l'application de supervision dans le binaire du relay.
//
// Un seul artefact à déployer : pas de serveur statique à part, pas de CORS, et
// aucun risque de désynchronisation entre le front et l'API qu'il consomme.
package relayui

import (
	"embed"
	"io/fs"
	"mime"
)

//go:embed index.html app.js sw.js manifest.webmanifest icon*.png icon*.svg
var files embed.FS

func init() {
	// Go ne connaît pas cette extension : sans cela le manifeste serait servi en
	// application/octet-stream et le navigateur refuserait d'installer la PWA.
	_ = mime.AddExtensionType(".webmanifest", "application/manifest+json")
}

// FS retourne les fichiers de l'application.
func FS() fs.FS { return files }
