package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store"
)

// maxLogRows caps the decision-log page (NFR-01).
const maxLogRows = 200

// logPage is the data for log.html (US-08, FR-21).
type logPage struct {
	page
	Kind          string // active filter, or "" for every kind
	Kinds         []logKindOption
	Group         string // active group: "", "venue", "staleness", "label"
	Groups        []logGroupOption
	Entries       []logEntryView
	GroupedEntries []logEntryGroup // When Group is set, entries grouped by criterion (UI-15)
	Outliers      *logOutliers // For routing entries only (UI-15)
	Limit         int
	Newest        time.Time // Raw UTC time of the newest entry
}

type logEntryGroup struct {
	Label   string
	Entries []logEntryView
}

type logKindOption struct {
	Value, Label string
	Selected     bool
}

type logGroupOption struct {
	Value, Label string
	Selected     bool
}

// logOutliers shows the worst saving and oldest input among displayed routing rows (UI-15).
type logOutliers struct {
	WorstSaving      *outlierEntry
	OldestInputAge   *outlierEntry
	OldestInputStamp string // Formatted age for display
}

type outlierEntry struct {
	Link  string
	About string
	Value string // Formatted value to show
}

// logEntryView is one decision_log row with the page it points at. Link is
// built by this package from a validated numeric ref, never from free text.
type logEntryView struct {
	ID            int64
	At            time.Time // Raw UTC time, formatted with formatTime in templates (M3-08)
	Kind          string
	KindChipClass string         // CSS chip class for the kind (UI-06)
	Ref           string
	Link          string
	LinkLabel     string         // Deprecated: use About instead (UI-06)
	About         string         // Human-readable "about" text (UI-06)
	AboutTitle    string         // Short title for the About link (UI-15)
	Summary       string
	// Routing-specific fields (UI-15), empty for other kinds
	Routing *routingLogView
}

// routingLogView holds enriched data for a routing decision log entry (UI-15).
// Populated from the stored decision when kind=routing.
type routingLogView struct {
	PairID         int64
	PairLink       string
	Side           string
	Size           string
	Type           string
	Outcome        Badge
	Venues         string // Comma-separated venue names
	AverageAllIn   string
	SavingVsBest   string // Sign-rule formatted: positive or (parentheses)
	HasInputs      bool   // True if decision has stored inputs (UI-25)
	MaxQuoteAge    *stampView
	Label          string // Relationship type at decision time (UI-25)
}

// stampView holds the data for rendering a freshness stamp (UI-15).
type stampView struct {
	Badge Badge
	Time  time.Time
	Age   time.Duration
}

// logKinds are the filter options, in display order.
var logKinds = []struct {
	kind  store.LogKind
	label string
}{
	{store.LogRelationship, "relationship verdicts"},
	{store.LogRouting, "routing decisions"},
	{store.LogLLM, "LLM verifier"},
	{store.LogIngest, "fetch rounds"},
}

// handleLog answers GET /log?kind=&group=: the newest decision-log entries across
// relationship verdicts, routing decisions, LLM verdicts and fetch rounds,
// each linked to the pair or stored decision it is about. UI-15 adds group-by and
// enriched routing columns.
func (s *Server) handleLog(w http.ResponseWriter, r *http.Request) {
	st, ok := s.storeOrFail(w)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	defer cancel()

	kind := store.LogKind(r.URL.Query().Get("kind"))
	if kind != "" && !kind.Valid() {
		s.renderErrorWithContext(w, r, http.StatusNotFound, "No such decision-log kind.")
		return
	}

	group := r.URL.Query().Get("group")
	// AB-08: validate group parameter
	validGroups := map[string]bool{"": true, "venue": true, "staleness": true, "label": true}
	if !validGroups[group] {
		s.renderErrorWithContext(w, r, http.StatusBadRequest, "Invalid group parameter.")
		return
	}

	round := roundFromContext(r.Context())
	tz := timezoneFromRequest(r)
	data := logPage{
		page:  s.pageBase(round, "Decision log", "What did Equinox decide recently, and why?", tz, "log"),
		Kind:  string(kind),
		Group: group,
		Limit: maxLogRows,
	}
	for _, k := range logKinds {
		data.Kinds = append(data.Kinds, logKindOption{Value: string(k.kind), Label: k.label, Selected: k.kind == kind})
	}
	// Group options (UI-15)
	groups := []struct{ value, label string }{
		{"", "None"},
		{"venue", "Venue"},
		{"staleness", "Staleness"},
		{"label", "Label"},
	}
	for _, g := range groups {
		data.Groups = append(data.Groups, logGroupOption{Value: g.value, Label: g.label, Selected: g.value == group})
	}

	entries, err := st.ListLog(ctx, store.LogFilter{Kind: kind, Limit: maxLogRows})
	if err != nil {
		s.dbError(w, "listing the decision log", err)
		return
	}

	// Enrich routing entries with decision data (UI-15)
	if kind == "" || kind == store.LogRouting {
		entries, err = s.enrichRoutingEntries(ctx, st, entries, round.ID)
		if err != nil {
			s.dbError(w, "enriching routing log entries", err)
			return
		}
	}

	data.Entries = make([]logEntryView, 0, len(entries))
	for _, e := range entries {
		data.Entries = append(data.Entries, s.newLogEntryView(e, tz))
	}

	if len(data.Entries) > 0 {
		data.Newest = data.Entries[0].At
	}

	// Compute outliers for routing entries (UI-15)
	if kind == store.LogRouting && len(data.Entries) > 0 {
		data.Outliers = s.computeOutliers(data.Entries, tz)
	}

	// Group entries if requested (UI-15)
	if group != "" && kind == store.LogRouting && len(data.Entries) > 0 {
		data.GroupedEntries = groupRoutingEntries(data.Entries, group)
	}

	s.render(w, http.StatusOK, "log", data)
}

func (s *Server) newLogEntryView(e store.LogEntry, tz string) logEntryView {
	v := logEntryView{
		ID:            e.ID,
		At:            e.CreatedAt,
		Kind:          string(e.Kind),
		KindChipClass: logKindChipClass(e.Kind),
		Ref:           e.Ref,
		Summary:       e.Summary,
	}
	v.Link, v.LinkLabel = logEntryLink(e.Kind, e.Ref)
	v.About = logEntryAbout(e) // Build from payload per UI-06
	v.AboutTitle = logEntryAboutTitle(e)

	// Parse routing-specific fields (UI-15)
	if e.Kind == store.LogRouting {
		v.Routing = s.parseRoutingLogPayload(e, tz)
	}

	return v
}

// parseRoutingLogPayload extracts routing-specific view data from an enriched log entry (UI-15).
func (s *Server) parseRoutingLogPayload(e store.LogEntry, tz string) *routingLogView {
	var payload struct {
		PairID        int64              `json:"pair_id"`
		Decision      canonical.Decision `json:"decision"`
		OutcomePhrase string             `json:"outcome_phrase"`
		OutcomeKey    string             `json:"outcome_key"`
		Venues        []string           `json:"venues"`
		AverageAllIn  string             `json:"average_all_in"`
		Saving        string             `json:"saving"`
		HasInputs     bool               `json:"has_inputs"`
		MaxQuoteAge   int64              `json:"max_quote_age_seconds,omitempty"`
		OldestFetch   *time.Time         `json:"oldest_fetch,omitempty"`
	}

	if err := json.Unmarshal(e.Payload, &payload); err != nil {
		return nil
	}

	d := payload.Decision

	// Pair link from payload (UI-15)
	pairID := payload.PairID
	pairLink := ""
	if pairID > 0 {
		pairLink = fmt.Sprintf("/pairs/%d", pairID)
	}

	// Format order details
	side := string(d.Order.Side)
	if side != "" {
		side = strings.ToUpper(side[:1]) + side[1:]
	}

	size := ""
	if d.Order.Quantity != nil {
		size = d.Order.Quantity.String()
	}

	orderType := string(d.Order.Type)

	// Max quote age stamp
	var maxQuoteAge *stampView
	if payload.HasInputs && payload.MaxQuoteAge > 0 && payload.OldestFetch != nil {
		age := time.Duration(payload.MaxQuoteAge) * time.Second
		freshState := FreshStateFresh
		// Apply stale threshold to max quote age (UI-15)
		if age > s.cfg.StaleAfter {
			freshState = FreshStateStale
		}
		maxQuoteAge = &stampView{
			Badge: FreshBadge(freshState),
			Time:  *payload.OldestFetch,
			Age:   age,
		}
	}

	// Label (target type from decision)
	label := string(d.TargetType)
	if label == "" {
		label = "label not stored"
	}

	return &routingLogView{
		PairID:       pairID,
		PairLink:     pairLink,
		Side:         side,
		Size:         size,
		Type:         orderType,
		Outcome:      OutcomeBadge(payload.OutcomeKey),
		Venues:       strings.Join(payload.Venues, ", "),
		AverageAllIn: payload.AverageAllIn,
		SavingVsBest: payload.Saving,
		HasInputs:    payload.HasInputs,
		MaxQuoteAge:  maxQuoteAge,
		Label:        label,
	}
}

// findPairIDForTarget attempts to find the pair ID for a given target instrument (UI-15).
// Returns 0 if not found. This is a best-effort lookup; failures are silent.
func (s *Server) findPairIDForTarget(targetID string) (int64, error) {
	// For now, return 0 to avoid complex query. The link will use decision ID instead.
	// TODO: Add store method to lookup pair by instrument ID if needed
	return 0, nil
}

// logKindChipClass maps a decision-log kind to its CSS chip class (UI-06, design.md § 4).
func logKindChipClass(kind store.LogKind) string {
	switch kind {
	case store.LogRelationship, store.LogLLM:
		return "chip-info" // match/llm both info
	case store.LogRouting:
		return "chip-ok" // routing is ok (green)
	case store.LogIngest:
		return "chip-muted" // ingest is muted
	default:
		return "chip-muted"
	}
}

// logEntryAbout builds the "about" text from the payload (UI-06).
// Relationship entries: "pair #N: venue1 / venue2" from a_id/b_id.
// Routing entries: "decision #N" + outcome phrase. Falls back to LinkLabel if payload doesn't parse.
func logEntryAbout(e store.LogEntry) string {
	_, label := logEntryLink(e.Kind, e.Ref)
	switch e.Kind {
	case store.LogRelationship, store.LogLLM:
		// Parse RelationshipPayload to get venue names from a_id/b_id
		var p struct {
			AID string `json:"a_id"`
			BID string `json:"b_id"`
		}
		if err := json.Unmarshal(e.Payload, &p); err != nil || p.AID == "" || p.BID == "" {
			return label // Fall back if payload doesn't parse
		}
		venueA := venueFromInstrumentID(p.AID)
		venueB := venueFromInstrumentID(p.BID)
		return label + ": " + venueA + " / " + venueB
	case store.LogRouting:
		// Try enriched payload first (UI-15)
		var enriched struct {
			OutcomePhrase string `json:"outcome_phrase"`
		}
		if err := json.Unmarshal(e.Payload, &enriched); err == nil && enriched.OutcomePhrase != "" {
			return label + ": " + enriched.OutcomePhrase
		}
		// Fall back to parsing Decision directly (for non-enriched entries)
		var d canonical.Decision
		if err := json.Unmarshal(e.Payload, &d); err != nil {
			return label // Fall back if payload doesn't parse
		}
		chosen := len(d.Chosen) > 0
		partial := chosen && d.Order.Quantity != nil && totalFilled(d.Chosen).LessThan(*d.Order.Quantity)
		isComplement := hasComplement(d.Chosen)
		venueCount := countVenues(d.Chosen)
		phrase, _ := OutcomePhrase(chosen, partial, isComplement, venueCount)
		return label + ": " + phrase
	default:
		return label
	}
}

// venueFromInstrumentID extracts the venue name from an instrument ID ("venue:id").
func venueFromInstrumentID(id string) string {
	if idx := strings.Index(id, ":"); idx > 0 {
		return id[:idx]
	}
	return "unknown"
}

// logEntryAboutTitle returns a short title for the AboutTitle field (UI-15).
func logEntryAboutTitle(e store.LogEntry) string {
	_, label := logEntryLink(e.Kind, e.Ref)
	return label
}

// enrichRoutingEntries fetches decisions for routing log entries and populates
// routing-specific fields (UI-15). Returns the entries with enriched payload.
func (s *Server) enrichRoutingEntries(ctx context.Context, st *store.Store, entries []store.LogEntry, currentRoundID int64) ([]store.LogEntry, error) {
	// Collect decision IDs from routing entries
	var decisionIDs []int64
	routingIndices := make(map[int64]int) // decision ID -> entry index
	for i, e := range entries {
		if e.Kind != store.LogRouting {
			continue
		}
		id, ok := parseID(e.Ref)
		if !ok {
			continue
		}
		decisionIDs = append(decisionIDs, id)
		routingIndices[id] = i
	}

	if len(decisionIDs) == 0 {
		return entries, nil
	}

	// Batch-fetch all routing decisions (one query for all listed rows per UI-15 spec)
	decisions, err := st.GetRoutingDecisionsBatch(ctx, decisionIDs)
	if err != nil {
		return nil, err
	}

	// Batch-fetch pair IDs for each decision's target instrument (UI-15)
	pairIDs := make(map[int64]int64) // decision ID -> pair ID
	for id, rd := range decisions {
		// Look up the pair for this target instrument
		rels, err := st.ListRelationships(ctx, store.RelationshipFilter{
			RoundID:      rd.Decision.RoundID,
			InstrumentID: rd.Decision.TargetID,
			Limit:        1,
		})
		if err == nil && len(rels) > 0 {
			pairIDs[id] = rels[0].ID
		}
	}

	// Enrich entries with decision data
	for id, idx := range routingIndices {
		rd, ok := decisions[id]
		if !ok {
			continue
		}
		// Get pair ID for this decision (0 if not found)
		pairID := pairIDs[id]
		// Replace payload with enriched version that includes routing view
		enriched, err := s.buildRoutingLogPayload(rd, currentRoundID, pairID)
		if err != nil {
			continue
		}
		entries[idx].Payload = enriched
	}

	return entries, nil
}

// buildRoutingLogPayload creates an enriched payload for a routing log entry (UI-15).
func (s *Server) buildRoutingLogPayload(rd store.RoutingDecision, currentRoundID int64, pairID int64) (json.RawMessage, error) {
	d := rd.Decision

	// Compute outcome
	chosen := len(d.Chosen) > 0
	partial := chosen && d.Order.Quantity != nil && totalFilled(d.Chosen).LessThan(*d.Order.Quantity)
	isComplement := hasComplement(d.Chosen)
	venueCount := countVenues(d.Chosen)
	phrase, _ := OutcomePhrase(chosen, partial, isComplement, venueCount)

	var outcomeKey string
	if !chosen {
		outcomeKey = "notrouted"
	} else if partial {
		outcomeKey = "partial"
	} else {
		outcomeKey = "routed"
	}

	// Collect venues
	venueSet := make(map[string]bool)
	for _, leg := range d.Chosen {
		venueSet[string(leg.Venue)] = true
	}
	var venues []string
	for v := range venueSet {
		venues = append(venues, v)
	}
	sort.Strings(venues)

	// Compute average all-in
	var avgAllIn decimal.Decimal
	if len(d.Chosen) > 0 {
		var totalCost decimal.Decimal
		totalFilled := decimal.Zero
		for _, leg := range d.Chosen {
			legCost := leg.VWAP.Add(leg.FeePerContract).Add(leg.BasisPenaltyPerContract)
			totalCost = totalCost.Add(legCost.Mul(leg.Quantity))
			totalFilled = totalFilled.Add(leg.Quantity)
		}
		if totalFilled.GreaterThan(decimal.Zero) {
			avgAllIn = totalCost.Div(totalFilled)
		}
	}

	// Compute saving vs best ask (UI-15, D-098 call 2)
	hasInputs := len(d.Inputs) > 0
	var savingStr string
	if hasInputs {
		// Find best ask from inputs (target itself has best price for the chosen side)
		var bestAsk decimal.Decimal
		found := false
		for _, inp := range d.Inputs {
			if inp.InstrumentID == d.TargetID {
				bestAsk = inp.BestAsk
				found = true
				break
			}
		}
		if found && avgAllIn.GreaterThan(decimal.Zero) {
			saving := bestAsk.Sub(avgAllIn)
			savingStr = formatSaving(saving)
		} else {
			savingStr = "—"
		}
	} else {
		savingStr = "not stored"
	}

	// Max quote age
	var maxQuoteAge time.Duration
	var oldestFetch time.Time
	if hasInputs {
		for _, inp := range d.Inputs {
			age := d.DecidedAt.Sub(inp.FetchedAt)
			if age > maxQuoteAge {
				maxQuoteAge = age
				oldestFetch = inp.FetchedAt
			}
		}
	}

	// Build enriched payload
	payload := struct {
		PairID       int64              `json:"pair_id"`
		Decision     canonical.Decision `json:"decision"`
		OutcomePhrase string            `json:"outcome_phrase"`
		OutcomeKey   string             `json:"outcome_key"`
		Venues       []string           `json:"venues"`
		AverageAllIn string             `json:"average_all_in"`
		Saving       string             `json:"saving"`
		HasInputs    bool               `json:"has_inputs"`
		MaxQuoteAge  int64              `json:"max_quote_age_seconds,omitempty"`
		OldestFetch  *time.Time         `json:"oldest_fetch,omitempty"`
	}{
		PairID:       pairID,
		Decision:     d,
		OutcomePhrase: phrase,
		OutcomeKey:   outcomeKey,
		Venues:       venues,
		AverageAllIn: formatPrice(avgAllIn),
		Saving:       savingStr,
		HasInputs:    hasInputs,
	}

	if hasInputs && maxQuoteAge > 0 {
		payload.MaxQuoteAge = int64(maxQuoteAge.Seconds())
		payload.OldestFetch = &oldestFetch
	}

	return json.Marshal(payload)
}

// formatSaving formats a saving per-contract difference with the sign rule (D-098 call 2).
// Positive is favorable, otherwise parentheses.
func formatSaving(saving decimal.Decimal) string {
	formatted := formatPrice(saving.Abs())
	if saving.GreaterThanOrEqual(decimal.Zero) {
		return formatted
	}
	return "(" + formatted + ")"
}

// computeOutliers finds the worst saving and oldest input among routing entries (UI-15).
func (s *Server) computeOutliers(entries []logEntryView, tz string) *logOutliers {
	var worstSaving *outlierEntry
	var oldestInput *outlierEntry
	worstSavingValue := decimal.RequireFromString("999999") // Start with a very high value
	var oldestInputAge time.Duration

	for _, e := range entries {
		if e.Routing == nil {
			continue
		}

		// Check for worst saving (most negative, or largest loss)
		if e.Routing.HasInputs && e.Routing.SavingVsBest != "—" && e.Routing.SavingVsBest != "not stored" {
			// Parse the saving value
			savingStr := e.Routing.SavingVsBest
			isNegative := false
			if strings.HasPrefix(savingStr, "(") && strings.HasSuffix(savingStr, ")") {
				isNegative = true
				savingStr = strings.Trim(savingStr, "()")
			}
			savingVal, err := decimal.NewFromString(savingStr)
			if err == nil {
				if isNegative {
					savingVal = savingVal.Neg()
				}
				if savingVal.LessThan(worstSavingValue) {
					worstSavingValue = savingVal
					worstSaving = &outlierEntry{
						Link:  e.Link,
						About: e.About,
						Value: e.Routing.SavingVsBest,
					}
				}
			}
		}

		// Check for oldest input
		if e.Routing.MaxQuoteAge != nil && e.Routing.MaxQuoteAge.Age > oldestInputAge {
			oldestInputAge = e.Routing.MaxQuoteAge.Age
			oldestInput = &outlierEntry{
				Link:  e.Link,
				About: e.About,
				Value: formatDuration(e.Routing.MaxQuoteAge.Age),
			}
		}
	}

	if worstSaving == nil && oldestInput == nil {
		return nil
	}

	return &logOutliers{
		WorstSaving:      worstSaving,
		OldestInputAge:   oldestInput,
		OldestInputStamp: formatDuration(oldestInputAge),
	}
}

// logEntryLink maps an entry to the page about it. The ref must be a valid
// path ID (AB-08); anything else gets no link.
func logEntryLink(kind store.LogKind, ref string) (href, label string) {
	id, ok := parseID(ref)
	if !ok {
		return "", ""
	}
	switch kind {
	case store.LogRelationship, store.LogLLM:
		return "/pairs/" + strconv.FormatInt(id, 10), "pair #" + ref
	case store.LogRouting:
		return "/decisions/" + strconv.FormatInt(id, 10), "decision #" + ref
	case store.LogIngest:
		return "", "round " + ref
	}
	return "", ""
}

// decisionPage is the data for decision.html: a stored routing decision
// rendered with the same trace as POST /route.
type decisionPage struct {
	routePage
	CreatedAt time.Time // Raw UTC time, formatted with formatTime in templates (M3-08)
}

// handleDecision answers GET /decisions/{id}: a stored routing decision
// (FR-17), linked from the decision log.
func (s *Server) handleDecision(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r.PathValue("id"))
	if !ok {
		s.renderErrorWithContext(w, r, http.StatusNotFound, "No such routing decision.")
		return
	}
	st, ok := s.storeOrFail(w)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	defer cancel()

	rd, err := st.GetRoutingDecision(ctx, id)
	switch {
	case isNotFound(err):
		s.renderErrorWithContext(w, r, http.StatusNotFound, "No such routing decision.")
		return
	case err != nil:
		s.dbError(w, "loading routing decision", err)
		return
	}
	round, err := st.GetRound(ctx, rd.RoundID)
	if err != nil && !isNotFound(err) {
		s.dbError(w, "loading the decision's round", err)
		return
	}
	snapshot := time.Time{}
	if err == nil {
		snapshot = round.SnapshotAt()
	}

	// Use current round from context for the page banner (not the decision's own round)
	currentRound := roundFromContext(r.Context())
	tz := timezoneFromRequest(r)
	data := decisionPage{
		routePage: routePage{
			page:       s.pageBase(currentRound, "Decision "+r.PathValue("id"), "What did this simulated order do, and why did the router choose that venue?", tz, "decision"),
			DecisionID: rd.ID,
			Decision:   newDecisionView(rd.Decision, countPartners(rd.Decision), snapshot, s.loadDecisionInstrumentTitles(ctx, st, rd.Decision), s.cfg.BasisPenaltyMult),
		},
		CreatedAt: rd.CreatedAt,
	}
	s.render(w, http.StatusOK, "decision", data)
}

// formatDuration returns a human-readable duration string (UI-15).
func formatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	s := int(d.Seconds())
	if s < 60 {
		return fmt.Sprintf("%d s", s)
	}
	m := s / 60
	if m < 60 {
		return fmt.Sprintf("%d min", m)
	}
	h := m / 60
	return fmt.Sprintf("%d h", h)
}

// formatPrice formats a decimal price for display (UI-15).
func formatPrice(p decimal.Decimal) string {
	return p.StringFixed(4)
}

// countPartners recovers how many partner instruments the router considered
// from a stored decision: every partner ends up as an alternative or an
// exclusion, and the target itself is neither a partner nor counted.
func countPartners(d canonical.Decision) int {
	seen := map[string]bool{}
	for _, alt := range d.Alternatives {
		for _, leg := range alt.Legs {
			seen[leg.InstrumentID] = true
		}
	}
	for _, ex := range d.Exclusions {
		seen[ex.InstrumentID] = true
	}
	delete(seen, d.TargetID)
	return len(seen)
}

// groupRoutingEntries groups routing log entries by the specified criterion (UI-15).
func groupRoutingEntries(entries []logEntryView, groupBy string) []logEntryGroup {
	if groupBy == "" || len(entries) == 0 {
		return nil
	}

	// Build map of group key -> entries
	groups := make(map[string][]logEntryView)
	for _, e := range entries {
		key := getGroupKey(e, groupBy)
		groups[key] = append(groups[key], e)
	}

	// Convert to sorted slice of groups
	var keys []string
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var result []logEntryGroup
	for _, k := range keys {
		result = append(result, logEntryGroup{
			Label:   k,
			Entries: groups[k],
		})
	}
	return result
}

// getGroupKey returns the group key for an entry based on the grouping criterion (UI-15).
func getGroupKey(e logEntryView, groupBy string) string {
	if e.Routing == nil {
		return "unknown"
	}

	switch groupBy {
	case "venue":
		if e.Routing.Venues == "" {
			return "not routed"
		}
		return e.Routing.Venues
	case "staleness":
		if e.Routing.MaxQuoteAge == nil {
			return "no input data"
		}
		if e.Routing.MaxQuoteAge.Badge.Class == "f-stale" {
			return "stale inputs"
		}
		return "fresh inputs"
	case "label":
		if e.Routing.Label == "" || e.Routing.Label == "label not stored" {
			return "label not stored"
		}
		return e.Routing.Label
	default:
		return "unknown"
	}
}
