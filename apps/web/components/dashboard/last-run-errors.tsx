"use client";

import { TriangleAlertIcon } from "lucide-react";
import { useTranslations } from "next-intl";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import type { PollRunError, StatusRepo } from "@/lib/query/types";

type LastRunErrorsProps = {
  errors: PollRunError[];
  repos: StatusRepo[];
};

export function LastRunErrors({
  errors,
  repos,
}: LastRunErrorsProps): React.ReactElement | null {
  const t = useTranslations("dashboard");

  if (errors.length === 0) {
    return null;
  }

  const repoPathById = new Map(
    repos.map((repo) => [repo.id, repo.projectPath]),
  );

  return (
    <Alert variant="warning">
      <TriangleAlertIcon />
      <AlertTitle>{t("lastRunErrorsTitle")}</AlertTitle>
      <AlertDescription>
        <ul className="list-disc ps-4">
          {errors.map((error) => {
            const path = repoPathById.get(error.repoId) ?? error.repoId;
            return (
              <li key={`${error.repoId}-${error.message}`}>
                <span className="font-medium font-mono">{path}</span>
                {": "}
                {error.message}
              </li>
            );
          })}
        </ul>
      </AlertDescription>
    </Alert>
  );
}
