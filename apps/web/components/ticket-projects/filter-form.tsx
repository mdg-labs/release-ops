import { InfoIcon } from "lucide-react";
import { useTranslations } from "next-intl";
import { useId } from "react";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Checkbox } from "@/components/ui/checkbox";
import { CheckboxGroup } from "@/components/ui/checkbox-group";
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import {
  FILTER_UPDATE_TYPES,
  isDefaultFilter,
  parseLabels,
} from "@/lib/ticket-projects/filter";
import type { FilterUpdateType, TicketProjectFilter } from "@/lib/query/types";

type FilterFormProps = {
  values: TicketProjectFilter;
  /** The labels field as typed; kept apart from values.labels so a trailing comma survives. */
  labelsText: string;
  onChange: (values: TicketProjectFilter) => void;
  onLabelsTextChange: (text: string) => void;
};

export function FilterForm({
  values,
  labelsText,
  onChange,
  onLabelsTextChange,
}: FilterFormProps): React.ReactElement {
  const t = useTranslations("ticket-projects");
  const labelsId = useId();
  const skipAutomergedId = useId();
  const current = { ...values, labels: parseLabels(labelsText) };

  return (
    <div className="flex flex-col gap-6">
      <Alert role="note" variant="info">
        <InfoIcon />
        <AlertTitle>{t("filter.noteTitle")}</AlertTitle>
        <AlertDescription>
          {isDefaultFilter(current)
            ? t("filter.noteDefault")
            : t("filter.noteCustom")}
        </AlertDescription>
      </Alert>

      <div className="flex flex-col items-start gap-2">
        <p className="font-medium text-sm">{t("filter.updateTypes")}</p>
        <CheckboxGroup
          aria-label={t("filter.updateTypes")}
          onValueChange={(next) =>
            onChange({ ...values, updateTypes: next as FilterUpdateType[] })
          }
          value={values.updateTypes}
        >
          {FILTER_UPDATE_TYPES.map((type) => (
            <Label key={type}>
              <Checkbox value={type} />
              {t(`filter.types.${type}`)}
            </Label>
          ))}
        </CheckboxGroup>
        <p className="text-muted-foreground text-xs">
          {t("filter.updateTypesHint")}
        </p>
      </div>

      <Field name="filterLabels">
        <FieldLabel htmlFor={labelsId}>{t("filter.labels")}</FieldLabel>
        <Input
          id={labelsId}
          onChange={(event) => onLabelsTextChange(event.target.value)}
          placeholder={t("filter.labelsPlaceholder")}
          value={labelsText}
        />
        <FieldDescription>{t("filter.labelsHint")}</FieldDescription>
      </Field>

      <Field name="filterSkipAutomerged">
        <div className="flex items-center gap-3">
          <Switch
            aria-labelledby={`${skipAutomergedId}-label`}
            checked={values.skipAutomerged}
            id={skipAutomergedId}
            onCheckedChange={(checked) =>
              onChange({ ...values, skipAutomerged: checked })
            }
          />
          <Label id={`${skipAutomergedId}-label`}>
            {t("filter.skipAutomerged")}
          </Label>
        </div>
        <FieldDescription>{t("filter.skipAutomergedHint")}</FieldDescription>
      </Field>
    </div>
  );
}
