import { getTranslations } from "next-intl/server";
import {
  Frame,
  FrameHeader,
  FramePanel,
  FrameTitle,
} from "@/components/ui/frame";

export default async function LoginPage(): Promise<React.ReactElement> {
  const t = await getTranslations("auth");

  return (
    <div className="flex min-h-svh items-center justify-center p-6">
      <div className="w-full max-w-sm">
        <Frame>
          <FramePanel>
            <FrameHeader>
              <FrameTitle>{t("login")}</FrameTitle>
            </FrameHeader>
          </FramePanel>
        </Frame>
      </div>
    </div>
  );
}
