package web

import (
	"bytes"
	"io"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// tailLines lit les n dernières lignes du fichier de log. La lecture se fait par
// blocs depuis la fin du fichier pour éviter de charger l'intégralité en mémoire.
func tailLines(path string, n int) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil // fichier pas encore créé : aucune ligne
		}
		return nil, err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	size := fi.Size()
	if size == 0 {
		return []string{}, nil
	}

	const chunk = 8192
	var buf []byte
	pos := size
	for pos > 0 && bytes.Count(buf, []byte{'\n'}) <= n {
		readSize := int64(chunk)
		if pos < readSize {
			readSize = pos
		}
		pos -= readSize
		tmp := make([]byte, readSize)
		if _, err := f.ReadAt(tmp, pos); err != nil && err != io.EOF {
			return nil, err
		}
		buf = append(tmp, buf...)
	}

	lines := strings.Split(strings.TrimRight(string(buf), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines, nil
}

// readNewLines lit les lignes complètes ajoutées au fichier depuis l'offset donné.
// Retourne les nouvelles lignes et le nouvel offset (position juste après la
// dernière ligne complète). Gère la troncature/rotation du fichier : si la taille
// est inférieure à l'offset, on repart de zéro.
func readNewLines(path string, offset int64) ([]string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, 0, nil
		}
		return nil, offset, err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return nil, offset, err
	}
	size := fi.Size()
	if size < offset {
		offset = 0 // fichier tronqué ou recréé
	}
	if size == offset {
		return nil, offset, nil
	}

	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, offset, err
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, offset, err
	}

	// On ne pousse que des lignes complètes : on s'arrête au dernier saut de ligne
	// et on garde l'éventuelle ligne partielle pour la prochaine lecture.
	idx := bytes.LastIndexByte(data, '\n')
	if idx == -1 {
		return nil, offset, nil
	}
	complete := data[:idx+1]
	newOffset := offset + int64(idx+1)
	lines := strings.Split(strings.TrimRight(string(complete), "\n"), "\n")
	return lines, newOffset, nil
}

// streamLogs ouvre un flux SSE qui pousse les nouvelles lignes du fichier de log
// au fil de l'eau. Le chargement initial (N dernières lignes) est assuré par
// GET /api/logs ; ce flux ne renvoie donc que ce qui est ajouté après connexion.
func streamLogs(c *gin.Context, path string) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no") // désactive le buffering d'un éventuel reverse-proxy

	// On démarre à la fin du fichier : seules les lignes ajoutées ensuite sont poussées.
	var offset int64
	if fi, err := os.Stat(path); err == nil {
		offset = fi.Size()
	}

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	c.Stream(func(w io.Writer) bool {
		<-ticker.C
		lines, newOffset, err := readNewLines(path, offset)
		if err != nil {
			return true // erreur transitoire : on réessaie au prochain tick
		}
		offset = newOffset
		for _, line := range lines {
			c.SSEvent("log", line)
		}
		return true
	})
}
