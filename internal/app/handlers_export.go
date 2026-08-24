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

// handleExportBoard downloads the version currently on screen as CSV.
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
		// A 200 is already on the wire, so there is no status left to change.
		a.Logger.Error("writing csv export", "err", err)
	}
}

// Keeps a ranking's free-text name from breaking out of the
// Content-Disposition header or colliding with a path on download.
var exportFilenameUnsafe = regexp.MustCompile(`[/\\:*?"<>|\x00-\x1f]`)

// exportFilename dates the download in UTC, not the server's local zone.
func exportFilename(rankingName string) string {
	name := strings.TrimSpace(exportFilenameUnsafe.ReplaceAllString(rankingName, ""))
	name = strings.TrimSpace(name)
	return fmt.Sprintf("%s %s.csv", cmp.Or(name, "Untitled ranking"), time.Now().UTC().Format("2006-01-02"))
}

// exportContentDisposition sends both filename forms: ASCII-only for clients
// that predate RFC 5987, and percent-encoded UTF-8 for those that follow it.
func exportContentDisposition(filename string) string {
	ascii := stripNonASCII(filename)
	return fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`, ascii, url.PathEscape(filename))
}

// stripNonASCII removes whole runes: every byte of a multi-byte UTF-8
// sequence has its high bit set, so none survives to be split.
func stripNonASCII(s string) string {
	b := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] < 0x80 {
			b = append(b, s[i])
		}
	}
	return string(b)
}
