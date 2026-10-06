package canonical

// QualityFlag names a data-quality problem found while reading external data.
type QualityFlag string

// Common quality flags. Adapters may add others; flags are free-form strings.
const (
	// QualityMissingField: a field the venue should send was absent.
	QualityMissingField QualityFlag = "missing_field"
	// QualityInvalidPrice: a price was unparseable or outside [0,1].
	QualityInvalidPrice QualityFlag = "invalid_price"
	// QualityInvalidSize: a size was unparseable, zero or negative.
	QualityInvalidSize QualityFlag = "invalid_size"
	// QualityInvalidTime: a timestamp was unparseable.
	QualityInvalidTime QualityFlag = "invalid_time"
	// QualityNoBook: the venue returned no order book; the quote is not routable.
	QualityNoBook QualityFlag = "no_book"
	// QualityDeadlineDefaulted: the deadline came from the close time, not the rules text.
	QualityDeadlineDefaulted QualityFlag = "deadline_defaulted"
	// QualityAmbiguousOutcome: a settled market's outcome could not be read as YES or NO.
	QualityAmbiguousOutcome QualityFlag = "ambiguous_outcome"
	// QualityRecordSkipped: one or more malformed records were dropped.
	QualityRecordSkipped QualityFlag = "record_skipped"
)

// QualityIssue is one quality flag with the field it concerns and a short,
// human-readable detail.
type QualityIssue struct {
	Flag   QualityFlag `json:"flag"`
	Field  string      `json:"field,omitempty"`
	Detail string      `json:"detail,omitempty"`
}

// Quality is the list of quality issues on a record. Empty means clean.
type Quality []QualityIssue

// maxQualityDetail caps a detail so external text cannot bloat stored records.
const maxQualityDetail = 200

// Add appends an issue, skipping an exact duplicate.
func (q *Quality) Add(flag QualityFlag, field, detail string) {
	issue := QualityIssue{Flag: flag, Field: field, Detail: truncate(detail, maxQualityDetail)}
	for _, existing := range *q {
		if existing == issue {
			return
		}
	}
	*q = append(*q, issue)
}

// Has reports whether any issue carries flag.
func (q Quality) Has(flag QualityFlag) bool {
	for _, issue := range q {
		if issue.Flag == flag {
			return true
		}
	}
	return false
}

// OK reports whether there are no quality issues.
func (q Quality) OK() bool { return len(q) == 0 }
