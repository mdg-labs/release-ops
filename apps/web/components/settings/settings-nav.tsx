"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { useTranslations } from "next-intl";
import { cn } from "@/lib/utils";

const SETTINGS_LINKS = [
  { href: "/settings", labelKey: "general" },
  { href: "/settings/users", labelKey: "usersNav" },
] as const;

export function SettingsNav(): React.ReactElement {
  const pathname = usePathname() ?? "";
  const t = useTranslations("settings");

  return (
    <nav
      aria-label={t("navAria")}
      className="flex flex-wrap gap-2 border-b pb-2"
    >
      {SETTINGS_LINKS.map((link) => {
        const isActive =
          link.href === "/settings"
            ? pathname === "/settings"
            : pathname.startsWith(link.href);

        return (
          <Link
            className={cn(
              "rounded-md px-3 py-1.5 font-medium text-sm transition-colors",
              isActive
                ? "bg-muted text-foreground"
                : "text-muted-foreground hover:bg-muted/60 hover:text-foreground",
            )}
            href={link.href}
            key={link.href}
          >
            {t(link.labelKey)}
          </Link>
        );
      })}
    </nav>
  );
}
