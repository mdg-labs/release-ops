"use client";

import { useTranslations } from "next-intl";
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field";
import {
  Select,
  SelectItem,
  SelectPopup,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import type { TicketMetadataItem } from "@/lib/query/types";

type MetadataSelectOption = {
  label: string;
  value: string;
};

type MetadataSelectProps = {
  id?: string;
  name: string;
  label: string;
  placeholder: string;
  value: string;
  onValueChange: (value: string) => void;
  items: TicketMetadataItem[];
  isLoading?: boolean;
  isError?: boolean;
  errorMessage?: string;
  disabled?: boolean;
  required?: boolean;
  description?: string;
  includeMissingValue?: boolean;
};

function toSelectItems(
  items: TicketMetadataItem[],
  selectedValue: string,
  includeMissingValue: boolean,
): MetadataSelectOption[] {
  const options = items.map((item) => ({
    value: item.id,
    label: item.label?.trim() || item.name,
  }));

  if (
    includeMissingValue &&
    selectedValue &&
    !options.some((option) => option.value === selectedValue)
  ) {
    options.unshift({ value: selectedValue, label: selectedValue });
  }

  return options;
}

export function MetadataSelect({
  id,
  name,
  label,
  placeholder,
  value,
  onValueChange,
  items,
  isLoading = false,
  isError = false,
  errorMessage,
  disabled = false,
  required = false,
  description,
  includeMissingValue = false,
}: MetadataSelectProps): React.ReactElement {
  const t = useTranslations("ticket-projects");
  const tCommon = useTranslations("common");

  const selectItems = toSelectItems(items, value, includeMissingValue);
  const selectedItem = selectItems.find((item) => item.value === value) ?? null;

  let helperText: string | null = null;
  if (isLoading) {
    helperText = tCommon("loading");
  } else if (isError) {
    helperText = t("metadata.loadFailed");
  } else if (errorMessage) {
    helperText = errorMessage;
  } else if (selectItems.length === 0) {
    helperText = t("metadata.empty");
  }

  return (
    <Field name={name}>
      <FieldLabel htmlFor={id}>
        {label} {required ? <span aria-hidden="true">*</span> : null}
      </FieldLabel>
      <Select
        disabled={disabled || isLoading || selectItems.length === 0}
        itemToStringValue={(item) => item.value}
        items={selectItems}
        onValueChange={(next) => {
          if (next) {
            onValueChange(next.value);
          }
        }}
        value={selectedItem}
      >
        <SelectTrigger id={id}>
          <SelectValue placeholder={placeholder} />
        </SelectTrigger>
        <SelectPopup>
          {selectItems.map((item) => (
            <SelectItem key={item.value} value={item}>
              {item.label}
            </SelectItem>
          ))}
        </SelectPopup>
      </Select>
      {description ? <FieldDescription>{description}</FieldDescription> : null}
      {helperText ? (
        <FieldDescription
          className={
            isError || errorMessage ? "text-destructive-foreground" : undefined
          }
        >
          {helperText}
        </FieldDescription>
      ) : null}
    </Field>
  );
}
