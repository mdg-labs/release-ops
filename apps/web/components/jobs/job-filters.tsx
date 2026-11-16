"use client";

import { useTranslations } from "next-intl";
import {
  Select,
  SelectItem,
  SelectPopup,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import type { JobState } from "@/lib/query/types";

export const JOB_STATES: readonly JobState[] = [
  "queued",
  "running",
  "succeeded",
  "failed",
];

export type RepositoryOption = { id: string; fullName: string };

type FilterItem = { label: string; value: string };

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

type JobFiltersProps = {
  repositories: RepositoryOption[];
  repositoryId: string;
  state: JobState | "";
  onRepositoryChange: (id: string) => void;
  onStateChange: (state: JobState | "") => void;
};

export function JobFilters({
  repositories,
  repositoryId,
  state,
  onRepositoryChange,
  onStateChange,
}: JobFiltersProps): React.ReactElement {
  const t = useTranslations("jobs");

  const repositoryItems: FilterItem[] = [
    { label: t("filters.allRepositories"), value: "" },
    ...repositories.map((repository) => ({
      label: repository.fullName,
      value: repository.id,
    })),
  ];
  const stateItems: FilterItem[] = [
    { label: t("filters.allStates"), value: "" },
    ...JOB_STATES.map((value) => ({
      label: t(`states.${value}`),
      value,
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
        items={stateItems}
        label={t("filters.state")}
        onChange={(value) => onStateChange(value as JobState | "")}
        value={state}
      />
    </div>
  );
}
