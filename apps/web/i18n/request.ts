import { getRequestConfig } from "next-intl/server";
import { getAppTimeZone } from "@/lib/format/timezone";

export default getRequestConfig(async () => {
  const locale = "en";

  return {
    locale,
    timeZone: getAppTimeZone(),
    messages: (await import(`../messages/${locale}.json`)).default,
  };
});
