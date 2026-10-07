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
} from "@/lib/ticket-projects/content-templates";
import type { ContentTemplates } from "@/lib/query/types";

type ContentTemplateFormProps = {
  kind: TicketIntegrationKind | undefined;
  values: ContentTemplates;
  onChange: (values: ContentTemplates) => void;
};

export function ContentTemplateForm({
  kind,
  values,
  onChange,
}: ContentTemplateFormProps): React.ReactElement {
  const t = useTranslations("ticket-projects");
  const titleId = useId();
  const descriptionId = useId();
  const supersedeCommentId = useId();
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
        <Field name="templateTitle">
          <FieldLabel htmlFor={titleId}>
            {t("templates.fields.title")}
          </FieldLabel>
          <Textarea
            className="font-mono"
            id={titleId}
            onChange={(event) =>
              onChange({ ...values, title: event.target.value })
            }
            placeholder={defaults.title}
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
            placeholder={defaults.description}
            rows={10}
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
            placeholder={defaults.supersedeComment}
            rows={5}
            value={values.supersedeComment}
          />
          <FieldDescription>{t("templates.supersedeHint")}</FieldDescription>
        </Field>
      </div>

      <section className="flex min-w-0 flex-col gap-2">
        <p className="font-medium text-sm">{t("templates.variablesTitle")}</p>
        <p className="text-muted-foreground text-sm">
          {t("templates.variablesDescription", {
            previousTag: templateVariableSyntax(".Previous.Tag"),
          })}
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
