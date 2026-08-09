import type React from "react";
import { cn } from "@/lib/utils";

type PageHeaderProps = {
  title: React.ReactNode;
  description?: React.ReactNode;
  action?: React.ReactNode;
  className?: string;
};

export function PageHeader({
  title,
  description,
  action,
  className,
}: PageHeaderProps): React.ReactElement {
  return (
    <div
      className={cn(
        "flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between",
        className,
      )}
      data-slot="page-header"
    >
      <div className="min-w-0 space-y-1">
        <h1 className="font-semibold text-2xl" data-slot="page-header-title">
          {title}
        </h1>
        {description ? (
          <p
            className="text-muted-foreground text-sm"
            data-slot="page-header-description"
          >
            {description}
          </p>
        ) : null}
      </div>
      {action ? (
        <div
          className="flex shrink-0 flex-wrap items-center gap-2"
          data-slot="page-header-action"
        >
          {action}
        </div>
      ) : null}
    </div>
  );
}
