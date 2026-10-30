package screening

// ExclusionScope is application-owned. QueryFingerprint makes an exclusion
// inapplicable after the identifying query fields change.
type ExclusionScope struct {
	TenantID, ObjectType, ObjectID, Provider, QueryFingerprint string
}

type ExclusionChange struct {
	Scope                                                ExclusionScope
	EntityID                                             string
	Active                                               bool
	OperationID, ScreeningID, MatchID, ActorID, Findings string
}
