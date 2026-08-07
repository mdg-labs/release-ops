import { getTranslations } from "next-intl/server";

export default async function ReposPage(): Promise<React.ReactElement> {
  const t = await getTranslations("repos");

  return (
    <div className="flex flex-col gap-2">
      <h1 className="font-semibold text-2xl">{t("title")}</h1>
    </div>
  );
}
