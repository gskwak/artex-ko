package server

import (
	"net/http"
	"strings"

	"github.com/Autumn-27/artex/db"
)

// blueListTraffic presents imported network events as defensive evidence.
// Capture and ingestion still happen through the existing Blue event import API.
func (s *Server) blueListTraffic(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	limit, offset, ok := blueLimitOffset(w, r)
	if !ok {
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	source := strings.TrimSpace(r.URL.Query().Get("source"))
	protocol := strings.TrimSpace(r.URL.Query().Get("protocol"))
	if len(query) > 200 || len(source) > 200 || len(protocol) > 80 {
		writeErr(w, http.StatusBadRequest, "traffic filter is too long")
		return
	}
	items, total, err := pg.ListBlueTraffic(r.Context(), db.BlueTrafficFilter{
		Query: query, Source: source, Protocol: protocol, Limit: limit, Offset: offset,
	})
	if err != nil {
		blueInternal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total})
}
