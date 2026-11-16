/** What "Enable repo" makes the new renovate.json extend when no preset repository is set. */
const DEFAULT_PRESET = "config:recommended";

/**
 * The preset the config opened by "Enable repo" extends: the first entry of
 * PRESET_REPOSITORIES written as local>owner/repo, else config:recommended
 * (docs/specs.html §7.5). The Go API decides; this names it in the confirmation.
 */
export function enableRepoPreset(presetRepositories: string[]): string {
  const first = presetRepositories[0];
  return first ? `local>${first}` : DEFAULT_PRESET;
}
