"use client";

import { useTranslations } from "next-intl";
import {
  Select,
  SelectItem,
  SelectPopup,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import type { UpdatePrType } from "@/lib/query/types";

export const UPDATE_TYPES: readonly UpdatePrType[] = [
  "major",
  "minor",
  "patch",
  "digest",
  "lockfile",
  "other",
];

export type RepositoryOption = { id: string; fullName: string };

type FilterItem = { label: string; value: string };

type UpdatePrFiltersProps = {
  repositories: RepositoryOption[];
  repositoryId: string;
  updateType: UpdatePrType | "";
  onRepositoryChange: (id: string) => void;
  onUpdateTypeChange: (type: UpdatePrType | "") => void;
};

function FilterSelect({
  label,
  items,
  value,
  onChange,
}: {
  label: string;
  items: FilterItem[];
  value: string;
  onChange: (value: string) => void;
}): React.ReactElement {
  const selected = items.find((item) => item.value === value) ?? items[0];

  return (
    <Select
      itemToStringValue={(item: FilterItem) => item.value}
      items={items}
      onValueChange={(next: FilterItem | null) => {
        if (next) {
          onChange(next.value);
        }
      }}
      value={selected}
    >
      <SelectTrigger aria-label={label} className="w-full sm:w-56" size="sm">
        <SelectValue />
      </SelectTrigger>
      <SelectPopup>
        {items.map((item) => (
          <SelectItem key={item.value} value={item}>
            {item.label}
          </SelectItem>
        ))}
      </SelectPopup>
    </Select>
  );
}

export function UpdatePrFilters({
  repositories,
  repositoryId,
  updateType,
  onRepositoryChange,
  onUpdateTypeChange,
}: UpdatePrFiltersProps): React.ReactElement {
  const t = useTranslations("dashboard");

  const repositoryItems: FilterItem[] = [
    { label: t("filters.allRepositories"), value: "" },
    ...repositories.map((repository) => ({
      label: repository.fullName,
      value: repository.id,
    })),
  ];
  const typeItems: FilterItem[] = [
    { label: t("filters.allUpdateTypes"), value: "" },
    ...UPDATE_TYPES.map((type) => ({
      label: t(`updateTypes.${type}`),
      value: type,
    })),
  ];

  return (
    <div className="flex flex-col gap-2 sm:flex-row">
      <FilterSelect
        items={repositoryItems}
        label={t("filters.repository")}
        onChange={onRepositoryChange}
        value={repositoryId}
      />
      <FilterSelect
        items={typeItems}
        label={t("filters.updateType")}
        onChange={(value) => onUpdateTypeChange(value as UpdatePrType | "")}
        value={updateType}
      />
    </div>
  );
}
