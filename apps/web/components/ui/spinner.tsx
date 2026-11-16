import { Loader2Icon } from "lucide-react";
import { useTranslations } from "next-intl";
import type React from "react";
import { cn } from "@/lib/utils";

export function Spinner({
  className,
  ...props
}: React.ComponentProps<typeof Loader2Icon>): React.ReactElement {
  const t = useTranslations("common");

  return (
    <Loader2Icon
      aria-label={t("loading")}
      className={cn("animate-spin", className)}
      role="status"
      {...props}
    />
  );
}
