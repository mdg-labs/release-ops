import { getTranslations } from "next-intl/server";
import { Button } from "@/components/ui/button";

export default async function HomePage() {
  const t = await getTranslations("common");

  return (
    <main className="flex min-h-screen flex-col items-center justify-center gap-4 p-8">
      <h1 className="text-2xl font-semibold">{t("appTitle")}</h1>
      <p className="text-muted-foreground">{t("scaffoldDescription")}</p>
      <Button type="button">{t("getStarted")}</Button>
    </main>
  );
}
