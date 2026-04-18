package gaps

// Remediation strings are concrete, actionable next steps keyed by gap
// rule. Each list is deliberately short so `aperture explain` renders
// without paginating and so the manifest's suggested_remediation array
// stays stable across runs.

func remediationMissingSpec() []string {
	return []string{
		"add a SPEC.md or AGENTS.md capturing the target behavior",
		"or pass --model with richer task text referencing the intended contract",
	}
}

func remediationMissingTests() []string {
	return []string{
		"add or broaden an existing _test.go so the test relationship scores above 0.50",
		"if tests live elsewhere (e.g. integration/), verify they are not excluded by .aperture.yaml",
	}
}

func remediationMissingConfigContext() []string {
	return []string{
		"ensure config/configs/*.yaml|toml|json|mk or go.mod/Makefile exists and is not excluded",
		"widen the task to cite the specific config key or file to disambiguate",
	}
}

func remediationUnresolvedSymbol(name string) []string {
	return []string{
		"confirm " + name + " is spelled correctly in the task",
		"run `aperture plan` after adding the file that exports " + name,
	}
}

func remediationAmbiguousOwnership(pkg string) []string {
	return []string{
		"name a specific file in " + pkg + " to raise its score past 0.80",
		"or ask the user which subsystem owner is expected",
	}
}

func remediationMissingRuntimePath() []string {
	return []string{
		"mention the request/response path or transport layer in the task",
		"verify the touched Go files carry io:* side-effects (i.e. import net/http, database/sql, etc.)",
	}
}

func remediationMissingExternalContract() []string {
	return []string{
		"add or unexclude the openapi / swagger / schema file the task references",
		"or restate the task without the contract wording if no wire contract exists",
	}
}

func remediationOversized() []string {
	return []string{
		"increase --budget",
		"narrow the task so fewer high-score files compete for the full slot",
		"or split the file so a smaller slice fits",
	}
}

func remediationTaskUnderspecified() []string {
	return []string{
		"add specific filenames, package names, or exported symbols to the task",
		"pick a verb from §7.3.1.1 so action_type resolves away from unknown",
	}
}
