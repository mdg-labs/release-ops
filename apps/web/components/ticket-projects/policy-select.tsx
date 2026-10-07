"use client";

import { useTranslations } from "next-intl";
import { Field, FieldLabel } from "@/components/ui/field";
import {
  Select,
  SelectItem,
  SelectPopup,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

export const ON_OPEN_TICKET_POLICIES = [
  "supersede",
  "merge",
  "skip_if_open",
] as const;

export type OnOpenTicketPolicy = (typeof ON_OPEN_TICKET_POLICIES)[number];

type PolicySelectProps = {
  value: OnOpenTicketPolicy;
  onValueChange: (value: OnOpenTicketPolicy) => void;
};

type PolicyOption = { label: string; value: OnOpenTicketPolicy };

export function PolicySelect({
  value,
  onValueChange,
}: PolicySelectProps): React.ReactElement {
  const t = useTranslations("ticket-projects");

  const items: PolicyOption[] = ON_OPEN_TICKET_POLICIES.map((policy) => ({
    label: t(`policies.${policy}`),
    value: policy,
  }));

  return (
    <Field name="onOpenTicketPolicy">
      <FieldLabel>{t("policy")}</FieldLabel>
      <Select
        itemToStringValue={(item) => item.value}
        items={items}
        onValueChange={(next) => {
          if (next) {
            onValueChange(next.value);
          }
        }}
        value={items.find((item) => item.value === value) ?? null}
      >
        <SelectTrigger>
          <SelectValue placeholder={t("policyPlaceholder")} />
        </SelectTrigger>
        <SelectPopup>
          {items.map((item) => (
            <SelectItem key={item.value} value={item}>
              {item.label}
            </SelectItem>
          ))}
        </SelectPopup>
      </Select>
    </Field>
  );
}
