package api

import (
	"net/http"
	"time"

	"github.com/foigent/x-insight/internal/executionid"
	"github.com/foigent/x-insight/internal/investigation"
)

func (s *Server) handlePosture(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowedFor(w, http.MethodGet)
		return
	}
	query, failure := s.parsePostureQuery(r)
	if failure != nil {
		writeRequestFailure(w, failure)
		return
	}
	store, ok := s.store.(PostureStore)
	if !ok {
		writeProblem(w, http.StatusServiceUnavailable, "database_unavailable", "Database is unavailable", true)
		return
	}
	snapshot, err := store.GetPosture(r.Context(), query)
	if err != nil {
		writeStoreFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
}

func (s *Server) parsePostureQuery(r *http.Request) (investigation.PostureQuery, *requestFailure) {
	if failure := validateQueryKeys(r, "as_of", "from", "to", "source_system_id"); failure != nil {
		return investigation.PostureQuery{}, failure
	}
	values := r.URL.Query()
	asOf, failure := s.parseAsOf(values.Get("as_of"), values.Has("as_of"))
	if failure != nil {
		return investigation.PostureQuery{}, failure
	}
	from := asOf.Add(-24 * time.Hour)
	to := asOf
	if values.Has("from") {
		from, failure = parseUTCField("from", values.Get("from"))
		if failure != nil {
			return investigation.PostureQuery{}, failure
		}
	}
	if values.Has("to") {
		to, failure = parseUTCField("to", values.Get("to"))
		if failure != nil {
			return investigation.PostureQuery{}, failure
		}
	}
	if !from.Before(to) {
		return investigation.PostureQuery{}, invalidField("from", "must_be_before_to")
	}
	if to.Sub(from) > 31*24*time.Hour {
		return investigation.PostureQuery{}, invalidField("from", "range_must_not_exceed_31_days")
	}
	if to.After(asOf) {
		return investigation.PostureQuery{}, invalidField("to", "must_not_be_after_as_of")
	}
	sourceSystemID := values.Get("source_system_id")
	if values.Has("source_system_id") {
		canonical, err := executionid.CanonicalSourceSystemID(sourceSystemID)
		if err != nil {
			return investigation.PostureQuery{}, invalidField("source_system_id", "must_be_a_uuid")
		}
		sourceSystemID = canonical
	}
	return investigation.PostureQuery{
		AsOf:           asOf.UTC(),
		From:           from.UTC(),
		To:             to.UTC(),
		SourceSystemID: sourceSystemID,
	}, nil
}
