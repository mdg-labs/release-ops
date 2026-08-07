export function buildIntegrationSecret(
  kind: string,
  token: string,
  email?: string,
): string {
  if (kind === "jira") {
    return JSON.stringify({ email: email ?? "", api_token: token });
  }
  if (kind === "phasical" || kind === "linear") {
    return JSON.stringify({ api_key: token });
  }
  return JSON.stringify({ token });
}
