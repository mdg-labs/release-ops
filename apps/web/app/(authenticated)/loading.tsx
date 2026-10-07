import { getTranslations } from "next-intl/server";
import { Spinner } from "@/components/ui/spinner";

export default async function AuthenticatedLoading(): Promise<React.ReactElement> {
  const t = await getTranslations("common");

  return (
    <div className="flex flex-1 items-center justify-center p-8">
      <Spinner aria-label={t("loading")} className="size-6" />
    </div>
  );
}
