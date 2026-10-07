"use client";

import { useTranslations } from "next-intl";
import { useEffect } from "react";

type PageTitleProps = {
  title: string;
  suffix?: string;
  hideAppName?: boolean;
};

export function PageTitle({
  title,
  suffix,
  hideAppName = false,
}: PageTitleProps): null {
  const t = useTranslations("common");
  const resolvedSuffix = suffix ?? t("appTitle");

  useEffect(() => {
    const formattedTitle = hideAppName
      ? title
      : resolvedSuffix
        ? `${title} — ${resolvedSuffix}`
        : title;
    document.title = formattedTitle;
  }, [title, resolvedSuffix, hideAppName]);

  return null;
}
