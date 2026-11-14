package inventory

// Rule names the reason Classify gave its answer. The set is closed and
// declared in temper/lattices/alerts.rules.yml.
type Rule string

const (
	RuleStocked Rule = "ok.stocked"
	RuleLow     Rule = "low.below_threshold"
	RuleOut     Rule = "out.zero"
	RuleOver    Rule = "over.capacity"
)

// Classify says how a SKU's stock stands, and which rule decided: out
// outranks over, which outranks low.
func Classify(stock, threshold, capacity float64) (string, Rule) {
	switch {
	case stock <= 0:
		return "out", RuleOut
	case stock > capacity:
		return "over", RuleOver
	case stock < threshold:
		return "low", RuleLow
	default:
		return "ok", RuleStocked
	}
}
