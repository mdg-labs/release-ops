"use client";

import { InfoIcon } from "lucide-react";
import { useTranslations } from "next-intl";
import { useId } from "react";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Textarea } from "@/components/ui/textarea";
import type { ContentTemplates } from "@/lib/query/types";

const TEMPLATE_VARIABLES = [
  "repoSourceKind",
  "repoProjectPath",
  "repoUrl",
  "releaseTag",
  "releaseName",
  "releaseUrl",
  "releaseNotes",
  "releasePublishedAt",
  "releaseIsPrerelease",
  "previousTag",
  "supersedeOldTag",
  "supersedeNewTag",
  "supersedeNewTicketUrl",
] as const;

type ContentTemplateFormProps = {
  values: ContentTemplates;
  onChange: (values: ContentTemplates) => void;
};

export function ContentTemplateForm({
  values,
  onChange,
}: ContentTemplateFormProps): React.ReactElement {
  const t = useTranslations("ticket-projects");
  const titleId = useId();
  const descriptionId = useId();
  const supersedeCommentId = useId();

  return (
    <div className="flex flex-col gap-4">
      <p className="font-medium text-sm">{t("templates.title")}</p>

      <Alert variant="warning">
        <InfoIcon />
        <AlertTitle>{t("templates.noteTitle")}</AlertTitle>
        <AlertDescription>{t("templates.noteDescription")}</AlertDescription>
      </Alert>

      <Field name="templateTitle">
        <FieldLabel htmlFor={titleId}>{t("templates.fields.title")}</FieldLabel>
        <Textarea
          className="font-mono"
          id={titleId}
          onChange={(event) =>
            onChange({ ...values, title: event.target.value })
          }
          placeholder={t("templates.placeholder")}
          value={values.title}
        />
      </Field>

      <Field name="templateDescription">
        <FieldLabel htmlFor={descriptionId}>
          {t("templates.fields.description")}
        </FieldLabel>
        <Textarea
          className="font-mono"
          id={descriptionId}
          onChange={(event) =>
            onChange({ ...values, description: event.target.value })
          }
          placeholder={t("templates.placeholder")}
          value={values.description}
        />
      </Field>

      <Field name="templateSupersedeComment">
        <FieldLabel htmlFor={supersedeCommentId}>
          {t("templates.fields.supersedeComment")}
        </FieldLabel>
        <Textarea
          className="font-mono"
          id={supersedeCommentId}
          onChange={(event) =>
            onChange({ ...values, supersedeComment: event.target.value })
          }
          placeholder={t("templates.placeholder")}
          value={values.supersedeComment}
        />
        <FieldDescription>{t("templates.supersedeHint")}</FieldDescription>
      </Field>

      <div className="flex flex-col gap-2">
        <p className="font-medium text-sm">{t("templates.variablesTitle")}</p>
        <p className="text-muted-foreground text-sm">
          {t("templates.variablesDescription")}
        </p>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t("templates.variablePath")}</TableHead>
              <TableHead>{t("templates.variableDescription")}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {TEMPLATE_VARIABLES.map((key) => (
              <TableRow key={key}>
                <TableCell className="font-mono text-sm">
                  {t(`templates.variables.${key}.path`)}
                </TableCell>
                <TableCell className="text-sm">
                  {t(`templates.variables.${key}.description`)}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
    </div>
  );
}
