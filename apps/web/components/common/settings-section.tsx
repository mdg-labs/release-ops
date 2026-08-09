import type React from "react";
import { FramePanel } from "@/components/ui/frame";
import { cn } from "@/lib/utils";

type SettingsSectionProps = {
  title: React.ReactNode;
  description?: React.ReactNode;
  icon?: React.ReactNode;
  children: React.ReactNode;
  className?: string;
};

export function SettingsSection({
  title,
  description,
  icon,
  children,
  className,
}: SettingsSectionProps): React.ReactElement {
  return (
    <section
      className={cn("flex flex-col gap-6", className)}
      data-slot="settings-section"
    >
      <div className="flex items-center gap-3">
        {icon ? (
          <div
            className="flex size-8 shrink-0 items-center justify-center rounded-md border bg-background [&_svg]:size-4"
            data-slot="settings-section-icon"
          >
            {icon}
          </div>
        ) : null}
        <div className="min-w-0 space-y-1">
          <h2
            className="font-semibold text-lg tracking-tight"
            data-slot="settings-section-title"
          >
            {title}
          </h2>
          {description ? (
            <p
              className="text-muted-foreground text-sm"
              data-slot="settings-section-description"
            >
              {description}
            </p>
          ) : null}
        </div>
      </div>
      <FramePanel
        className="flex flex-col gap-4"
        data-slot="settings-section-panel"
      >
        {children}
      </FramePanel>
    </section>
  );
}
