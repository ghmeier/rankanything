package app

import (
	"cmp"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/ghmeier/rankanything/internal/constants"
	"github.com/ghmeier/rankanything/internal/db"
	"github.com/ghmeier/rankanything/internal/services"
	"github.com/google/uuid"
)

// handleExportBoard is GET /r/{uuid}/export or /r/{uuid}/v/{short}/export —
// download the version RequireRankingAccess resolved (the one currently on
// screen, live or pinned) as CSV. This is a read, so unlike the mutating
// board routes it only needs RequireRankingAccess, not requireDraftVersion.
func (a *App) handleExportBoard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rankingUUID := ctx.Value(constants.RankingUUIDKey).(uuid.UUID)
	version := ctx.Value(constants.RankingVersionKey).(db.RankingVersion)

	ranking, err := a.RankingSvc.GetRanking(ctx, rankingUUID)
	if err != nil {
		rankError(a, w, r, err)
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
		// Headers and a 200 are already on the wire, so the only thing left
		// to do with a write failure (client disconnect, broken pipe) is log
		// it — there's no status left to change.
		a.Logger.Error("writing csv export", "err", err)
	}
}

// exportFilenameUnsafe matches characters that are unsafe in a filename —
// path separators, quotes, and control characters — so a ranking's
// free-text name can't break out of the Content-Disposition header or
// collide with a path when a browser saves the download.
var exportFilenameUnsafe = regexp.MustCompile(`[/\\:*?"<>|\x00-\x1f]`)

// exportFilename builds the CSV download's filename from the ranking's name
// and today's date (UTC, so the name doesn't depend on the server's local
// time zone).
func exportFilename(rankingName string) string {
	name := strings.TrimSpace(exportFilenameUnsafe.ReplaceAllString(rankingName, ""))
	name = strings.TrimSpace(name)
	return fmt.Sprintf("%s %s.csv", cmp.Or(name, "Untitled ranking"), time.Now().UTC().Format("2006-01-02"))
}

// exportContentDisposition builds an attachment header carrying both the
// plain filename= form and the RFC 5987/6266 filename*= form. filename= is
// ASCII-only — non-ASCII characters are dropped rather than mangled, for
// clients that don't understand filename*= — while filename*= carries the
// full, percent-encoded UTF-8 name, so a ranking name with non-ASCII
// characters survives in every browser that follows the RFC.
func exportContentDisposition(filename string) string {
	ascii := stripNonASCII(filename)
	return fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`, ascii, url.PathEscape(filename))
}

// stripNonASCII drops every byte at or above 0x80. Every byte of a
// multi-byte UTF-8 sequence has its high bit set, so this always removes
// whole runes rather than splitting one, leaving a plain ASCII string safe
// to place in a quoted header value.
func stripNonASCII(s string) string {
	b := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] < 0x80 {
			b = append(b, s[i])
		}
	}
	return string(b)
}
