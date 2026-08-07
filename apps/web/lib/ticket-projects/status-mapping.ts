export type StatusMappingValues = {
  open: string[];
  done: string[];
  cancelled: string[];
  superseded: string;
};

export function defaultStatusMapping(): StatusMappingValues {
  return {
    open: [],
    done: [],
    cancelled: [],
    superseded: "",
  };
}

export function parseStatusList(value: string): string[] {
  return value
    .split(",")
    .map((item) => item.trim())
    .filter((item) => item.length > 0);
}

export function formatStatusList(values: string[]): string {
  return values.join(", ");
}

export function statusMappingFromRecord(
  record: Record<string, unknown>,
): StatusMappingValues {
  return {
    open: Array.isArray(record.open)
      ? record.open.filter((item): item is string => typeof item === "string")
      : [],
    done: Array.isArray(record.done)
      ? record.done.filter((item): item is string => typeof item === "string")
      : [],
    cancelled: Array.isArray(record.cancelled)
      ? record.cancelled.filter(
          (item): item is string => typeof item === "string",
        )
      : [],
    superseded: typeof record.superseded === "string" ? record.superseded : "",
  };
}

export function statusMappingToRecord(
  values: StatusMappingValues,
): Record<string, unknown> {
  return {
    open: values.open,
    done: values.done,
    cancelled: values.cancelled,
    superseded: values.superseded,
  };
}
