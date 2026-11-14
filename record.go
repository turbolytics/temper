package temper

// Record is one evidence record: what a test proves and how it ended.
//
// A record carries exactly one of Covers (the test proves the claim) or
// Exempts (the test proves the subject is legitimately excused from it).
// The field order fixes the JSON key order, so records written by any
// adapter read the same.
type Record struct {
	Covers   string `json:"covers,omitempty"`
	Exempts  string `json:"exempts,omitempty"`
	Subject  string `json:"subject,omitempty"`
	Scenario string `json:"scenario,omitempty"`
	Test     string `json:"test"`
	Level    string `json:"level"`
	Outcome  string `json:"outcome"`
	Run      string `json:"run,omitempty"`
	At       string `json:"at,omitempty"`
}

// The outcomes a record can carry. Only OutcomePass counts as evidence (R3).
const (
	OutcomePass  = "pass"
	OutcomeFail  = "fail"
	OutcomeSkip  = "skip"
	OutcomeError = "error"
)
