package agent

// apertureEnv returns the §7.10.4.1 base environment-variable set. Any
// non-empty field in req is exported; unset fields are omitted so
// downstream adapters can detect "not supplied" correctly.
func apertureEnv(req RunRequest) []string {
	var out []string
	add := func(k, v string) {
		if v == "" {
			return
		}
		out = append(out, k+"="+v)
	}
	add("APERTURE_MANIFEST_PATH", req.ManifestJSONPath)
	add("APERTURE_MANIFEST_MARKDOWN_PATH", req.ManifestMarkdownPath)
	add("APERTURE_TASK_PATH", req.TaskPath)
	add("APERTURE_PROMPT_PATH", req.PromptPath)
	add("APERTURE_REPO_ROOT", req.RepoRoot)
	add("APERTURE_MANIFEST_HASH", req.ManifestHash)
	add("APERTURE_VERSION", req.ApertureVersion)
	for k, v := range req.AgentConfig.Env {
		out = append(out, k+"="+v)
	}
	return out
}
