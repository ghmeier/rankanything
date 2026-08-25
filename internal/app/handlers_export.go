package app

import (
	"cmp"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/ghmeier/rankanything/internal/services"
)

func (a *App) handleExportBoard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rankingUUID, version := boardScope(r)

	ranking, err := a.RankingSvc.GetRanking(ctx, rankingUUID)
	if err != nil {
		a.rankError(w, r, err)
		return
	}
	board, err := a.RankingSvc.GetBoard(ctx, ranking, version)
	if err != nil {
		a.serverError(w, r, err)
		return
	}

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", exportContentDisposition(exportFilename(ranking.Name)))
	w.WriteHeader(http.StatusOK)
	if err := services.WriteBoardCSV(w, board); err != nil {
		// Status already sent; nothing to change.
		a.Logger.Error("writing csv export", "err", err)
	}
}

var exportFilenameUnsafe = regexp.MustCompile(`[/\\:*?"<>|\x00-\x1f]`)

func exportFilename(rankingName string) string {
	name := strings.TrimSpace(exportFilenameUnsafe.ReplaceAllString(rankingName, ""))
	name = strings.TrimSpace(name)
	return fmt.Sprintf("%s %s.csv", cmp.Or(name, "Untitled ranking"), time.Now().UTC().Format("2006-01-02"))
}

// ASCII fallback for pre-RFC 5987 clients, UTF-8 for modern ones.
func exportContentDisposition(filename string) string {
	ascii := stripNonASCII(filename)
	return fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`, ascii, url.PathEscape(filename))
}

func stripNonASCII(s string) string {
	b := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] < 0x80 {
			b = append(b, s[i])
		}
	}
	return string(b)
}
