package manifest

type LoadMode string

const (
	LoadModeFull              LoadMode = "full"
	LoadModeStructuralSummary LoadMode = "structural_summary"
	LoadModeBehavioralSummary LoadMode = "behavioral_summary"
	LoadModeReachable         LoadMode = "reachable"
)

type ActionType string

const (
	ActionTypeBugfix        ActionType = "bugfix"
	ActionTypeFeature       ActionType = "feature"
	ActionTypeRefactor      ActionType = "refactor"
	ActionTypeTestAddition  ActionType = "test-addition"
	ActionTypeDocumentation ActionType = "documentation"
	ActionTypeInvestigation ActionType = "investigation"
	ActionTypeMigration     ActionType = "migration"
	ActionTypeUnknown       ActionType = "unknown"
)

type GapType string

const (
	GapMissingSpec                GapType = "missing_spec"
	GapMissingTests               GapType = "missing_tests"
	GapMissingConfigContext       GapType = "missing_config_context"
	GapUnresolvedSymbolDependency GapType = "unresolved_symbol_dependency"
	GapAmbiguousOwnership         GapType = "ambiguous_ownership"
	GapMissingRuntimePath         GapType = "missing_runtime_path"
	GapMissingExternalContract    GapType = "missing_external_contract"
	GapOversizedPrimaryContext    GapType = "oversized_primary_context"
	GapTaskUnderspecified         GapType = "task_underspecified"
)

type GapSeverity string

const (
	GapSeverityInfo     GapSeverity = "info"
	GapSeverityWarning  GapSeverity = "warning"
	GapSeverityBlocking GapSeverity = "blocking"
)

const (
	SchemaVersion         = "1.0"
	SelectionLogicVersion = "sel-v1"
	SideEffectTablesVer   = "side-effect-tables-v1"
)

type Manifest struct {
	SchemaVersion      string             `json:"schema_version"`
	ManifestID         string             `json:"manifest_id"`
	ManifestHash       string             `json:"manifest_hash"`
	GeneratedAt        string             `json:"generated_at"`
	Incomplete         bool               `json:"incomplete"`
	Task               Task               `json:"task"`
	Repo               Repo               `json:"repo"`
	Budget             Budget             `json:"budget"`
	Selections         []Selection        `json:"selections"`
	Reachable          []Reachable        `json:"reachable"`
	Exclusions         []Exclusion        `json:"exclusions"`
	Gaps               []Gap              `json:"gaps"`
	Feasibility        Feasibility        `json:"feasibility"`
	GenerationMetadata GenerationMetadata `json:"generation_metadata"`
}

type Task struct {
	TaskID             string     `json:"task_id"`
	Source             string     `json:"source"`
	RawText            string     `json:"raw_text"`
	Type               ActionType `json:"type"`
	Objective          string     `json:"objective"`
	Anchors            []string   `json:"anchors"`
	ExpectsTests       bool       `json:"expects_tests"`
	ExpectsConfig      bool       `json:"expects_config"`
	ExpectsDocs        bool       `json:"expects_docs"`
	ExpectsMigration   bool       `json:"expects_migration"`
	ExpectsAPIContract bool       `json:"expects_api_contract"`
}

type Repo struct {
	Root          string   `json:"root"`
	Fingerprint   string   `json:"fingerprint"`
	LanguageHints []string `json:"language_hints"`
}

type Budget struct {
	Model                   string   `json:"model"`
	TokenCeiling            int      `json:"token_ceiling"`
	Reserved                Reserved `json:"reserved"`
	EffectiveContextBudget  int      `json:"effective_context_budget"`
	EstimatedSelectedTokens int      `json:"estimated_selected_tokens"`
	Estimator               string   `json:"estimator"`
	EstimatorVersion        string   `json:"estimator_version"`
}

type Reserved struct {
	Instructions int `json:"instructions"`
	Reasoning    int `json:"reasoning"`
	ToolOutput   int `json:"tool_output"`
	Expansion    int `json:"expansion"`
}

type Selection struct {
	Path            string           `json:"path"`
	Kind            string           `json:"kind"`
	LoadMode        LoadMode         `json:"load_mode"`
	RelevanceScore  float64          `json:"relevance_score"`
	ScoreBreakdown  []BreakdownEntry `json:"score_breakdown"`
	EstimatedTokens int              `json:"estimated_tokens"`
	Rationale       []string         `json:"rationale"`
	DemotionReason  *string          `json:"demotion_reason,omitempty"`
	SideEffects     []string         `json:"side_effects"`
}

type BreakdownEntry struct {
	Factor       string  `json:"factor"`
	Signal       float64 `json:"signal"`
	Weight       float64 `json:"weight"`
	Contribution float64 `json:"contribution"`
}

type Reachable struct {
	Path           string           `json:"path"`
	RelevanceScore float64          `json:"relevance_score"`
	Reason         string           `json:"reason"`
	ScoreBreakdown []BreakdownEntry `json:"score_breakdown"`
}

type Exclusion struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

type Gap struct {
	ID                   string      `json:"id"`
	Type                 GapType     `json:"type"`
	Severity             GapSeverity `json:"severity"`
	Description          string      `json:"description"`
	Evidence             []string    `json:"evidence"`
	SuggestedRemediation []string    `json:"suggested_remediation"`
}

type Feasibility struct {
	Score              float64            `json:"score"`
	Assessment         string             `json:"assessment"`
	Positives          []string           `json:"positives"`
	Negatives          []string           `json:"negatives"`
	BlockingConditions []string           `json:"blocking_conditions"`
	SubSignals         map[string]float64 `json:"sub_signals"`
}

type GenerationMetadata struct {
	ApertureVersion         string `json:"aperture_version"`
	SelectionLogicVersion   string `json:"selection_logic_version"`
	ConfigDigest            string `json:"config_digest"`
	SideEffectTablesVersion string `json:"side_effect_tables_version"`
	Host                    string `json:"host"`
	PID                     int    `json:"pid"`
	WallClockStartedAt      string `json:"wall_clock_started_at"`
}
