import { CopyIcon, InfoIcon } from "lucide-react";
import { useTranslations } from "next-intl";
import { useId } from "react";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
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
import { toastManager } from "@/components/ui/toast";
import type { TicketIntegrationKind } from "@/lib/integrations/kinds";
import {
  TEMPLATE_VARIABLES,
  integrationDefaultTemplates,
  templateVariableSyntax,
  type TemplateField,
} from "@/lib/ticket-projects/content-templates";
import { renderTemplatePreview } from "@/lib/ticket-projects/template-preview";
import type { ContentTemplates } from "@/lib/query/types";

type ContentTemplateFormProps = {
  kind: TicketIntegrationKind | undefined;
  values: ContentTemplates;
  onChange: (values: ContentTemplates) => void;
};

const TEMPLATE_FIELDS: readonly { key: TemplateField; rows: number }[] = [
  { key: "title", rows: 3 },
  { key: "description", rows: 10 },
  { key: "updateComment", rows: 5 },
  { key: "supersedeComment", rows: 5 },
];

type TemplateTextareaProps = {
  name: TemplateField;
  value: string;
  defaultValue: string;
  rows: number;
  hint: string;
  onChange: (value: string) => void;
};

// A template field with the variables it has and a preview of what it renders from sample
// data; an empty field previews the integration default it falls back to.
function TemplateTextarea({
  name,
  value,
  defaultValue,
  rows,
  hint,
  onChange,
}: TemplateTextareaProps): React.ReactElement {
  const t = useTranslations("ticket-projects");
  const id = useId();
  const preview = renderTemplatePreview(
    value.trim() ? value : defaultValue,
    name,
  );

  return (
    <Field name={`template-${name}`}>
      <FieldLabel htmlFor={id}>{t(`templates.fields.${name}`)}</FieldLabel>
      <Textarea
        className="font-mono"
        id={id}
        onChange={(event) => onChange(event.target.value)}
        placeholder={defaultValue}
        rows={rows}
        value={value}
      />
      <FieldDescription>{hint}</FieldDescription>
      <div className="flex w-full flex-col gap-1">
        <p className="font-medium text-xs">{t("templates.previewTitle")}</p>
        {preview.ok ? (
          <pre
            aria-label={t("templates.previewAria", {
              field: t(`templates.fields.${name}`),
            })}
            className="max-h-48 overflow-auto whitespace-pre-wrap break-words rounded-lg border bg-muted/40 p-3 font-mono text-xs"
          >
            {preview.text}
          </pre>
        ) : (
          <p className="text-muted-foreground text-xs">
            {t(`templates.previewUnavailable.${preview.reason}`)}
          </p>
        )}
      </div>
    </Field>
  );
}

export function ContentTemplateForm({
  kind,
  values,
  onChange,
}: ContentTemplateFormProps): React.ReactElement {
  const t = useTranslations("ticket-projects");
  const defaults = integrationDefaultTemplates(kind);

  async function copyVariable(syntax: string): Promise<void> {
    try {
      await navigator.clipboard.writeText(syntax);
      toastManager.add({
        title: t("templates.copied", { syntax }),
        type: "success",
      });
    } catch {
      toastManager.add({
        title: t("templates.copyFailed"),
        type: "error",
      });
    }
  }

  return (
    <div className="flex flex-col gap-6">
      <Alert variant="warning">
        <InfoIcon />
        <AlertTitle>{t("templates.noteTitle")}</AlertTitle>
        <AlertDescription>{t("templates.noteDescription")}</AlertDescription>
      </Alert>

      <div className="flex flex-col gap-4">
        {TEMPLATE_FIELDS.map((field) => (
          <TemplateTextarea
            defaultValue={defaults[field.key]}
            hint={t(`templates.hints.${field.key}`)}
            key={field.key}
            name={field.key}
            onChange={(value) => onChange({ ...values, [field.key]: value })}
            rows={field.rows}
            value={values[field.key]}
          />
        ))}
      </div>

      <section className="flex min-w-0 flex-col gap-2">
        <p className="font-medium text-sm">{t("templates.variablesTitle")}</p>
        <p className="text-muted-foreground text-sm">
          {t("templates.variablesDescription")}
        </p>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t("templates.variablePath")}</TableHead>
              <TableHead>{t("templates.variableDescription")}</TableHead>
              <TableHead>{t("templates.variableWorksIn")}</TableHead>
              <TableHead className="w-10">
                <span className="sr-only">{t("templates.variableCopy")}</span>
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {TEMPLATE_VARIABLES.map((variable) => {
              const syntax = templateVariableSyntax(variable.path);
              return (
                <TableRow key={variable.key}>
                  <TableCell className="font-mono text-xs">{syntax}</TableCell>
                  <TableCell className="whitespace-normal text-xs">
                    {t(`templates.variables.${variable.key}.description`)}
                  </TableCell>
                  <TableCell className="whitespace-normal text-xs">
                    {variable.worksIn
                      .map((field) => t(`templates.worksIn.${field}`))
                      .join(", ")}
                  </TableCell>
                  <TableCell>
                    <Button
                      aria-label={t("templates.copyAria", {
                        path: variable.path,
                      })}
                      onClick={() => void copyVariable(syntax)}
                      size="icon-sm"
                      type="button"
                      variant="ghost"
                    >
                      <CopyIcon />
                    </Button>
                  </TableCell>
                </TableRow>
              );
            })}
          </TableBody>
        </Table>
      </section>
    </div>
  );
}
