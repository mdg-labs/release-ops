/**
 * Posts the App manifest to GitHub's registration page the way GitHub's manifest flow
 * requires: a form POST in the browser with the manifest as a JSON string in the field
 * `manifest`. The navigation leaves this app, so there is nothing to return.
 */
export function submitManifestForm(
  registrationUrl: string,
  manifest: Record<string, unknown>,
): void {
  const form = document.createElement("form");
  form.method = "post";
  form.action = registrationUrl;
  form.hidden = true;

  const field = document.createElement("input");
  field.type = "hidden";
  field.name = "manifest";
  field.value = JSON.stringify(manifest);
  form.appendChild(field);

  document.body.appendChild(form);
  form.submit();
}
