"use client";

import { CircleCheckIcon, KeyRoundIcon } from "lucide-react";
import { useTranslations } from "next-intl";
import { useId, useState } from "react";
import { SettingsSection } from "@/components/common/settings-section";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field";
import { Spinner } from "@/components/ui/spinner";
import { Textarea } from "@/components/ui/textarea";
import { ApiError, NetworkError } from "@/lib/api/client";
import { useRotateGitHubAppKey } from "@/lib/hooks/use-github-app-key";

type ErrorKey =
  | "keyRejected"
  | "githubUnavailable"
  | "notConfigured"
  | "forbidden"
  | "network"
  | "unknown"
  | "fileUnreadable";

function errorKeyFor(error: unknown): ErrorKey {
  if (error instanceof ApiError) {
    switch (error.code) {
      case "VALIDATION_ERROR":
        return "keyRejected";
      case "GITHUB_UNAVAILABLE":
        return "githubUnavailable";
      case "NOT_FOUND":
        return "notConfigured";
      case "FORBIDDEN":
        return "forbidden";
      default:
        return "unknown";
    }
  }
  if (error instanceof NetworkError) {
    return "network";
  }
  return "unknown";
}

export function GitHubAppKeySection(): React.ReactElement {
  const t = useTranslations("githubAppKey");
  const fileId = useId();
  const rotate = useRotateGitHubAppKey();
  const [privateKey, setPrivateKey] = useState("");
  const [error, setError] = useState<ErrorKey | null>(null);
  const [rotated, setRotated] = useState(false);

  function edit(value: string): void {
    setPrivateKey(value);
    setError(null);
    setRotated(false);
  }

  async function handleFile(file: File | undefined): Promise<void> {
    if (!file) {
      return;
    }
    try {
      edit(await file.text());
    } catch {
      setError("fileUnreadable");
    }
  }

  async function handleSubmit(): Promise<void> {
    setError(null);
    setRotated(false);
    try {
      await rotate.mutateAsync(privateKey);
      setPrivateKey("");
      setRotated(true);
    } catch (caught) {
      setError(errorKeyFor(caught));
    }
  }

  return (
    <SettingsSection
      description={t("description")}
      icon={<KeyRoundIcon aria-hidden="true" />}
      title={t("title")}
    >
      <form
        className="flex flex-col gap-4"
        onSubmit={(event) => {
          event.preventDefault();
          void handleSubmit();
        }}
      >
        <Field>
          <FieldLabel htmlFor={fileId}>{t("fileLabel")}</FieldLabel>
          <input
            accept=".pem,text/plain"
            className="text-sm"
            disabled={rotate.isPending}
            id={fileId}
            onChange={(event) => {
              void handleFile(event.target.files?.[0]);
              event.target.value = "";
            }}
            type="file"
          />
        </Field>
        <Field>
          <FieldLabel>{t("keyLabel")}</FieldLabel>
          <Textarea
            autoComplete="off"
            className="font-mono"
            disabled={rotate.isPending}
            onChange={(event) => edit(event.target.value)}
            placeholder={t("keyPlaceholder")}
            rows={6}
            spellCheck={false}
            value={privateKey}
          />
          <FieldDescription>{t("keyHint")}</FieldDescription>
        </Field>
        {rotated ? (
          <Alert variant="success">
            <CircleCheckIcon />
            <AlertTitle>{t("rotated")}</AlertTitle>
            <AlertDescription>{t("rotatedDescription")}</AlertDescription>
          </Alert>
        ) : null}
        {error ? (
          <Alert variant="error">
            <AlertTitle>
              {error === "network" || error === "unknown"
                ? t("unconfirmed")
                : t("failed")}
            </AlertTitle>
            <AlertDescription>{t(`errors.${error}`)}</AlertDescription>
          </Alert>
        ) : null}
        <div>
          <Button
            disabled={rotate.isPending || privateKey.trim() === ""}
            type="submit"
          >
            {rotate.isPending ? <Spinner className="size-4" /> : null}
            {rotate.isPending ? t("verifying") : t("submit")}
          </Button>
        </div>
      </form>
    </SettingsSection>
  );
}
