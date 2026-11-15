import { AdminOnly } from "@/components/common/admin-only";
import { SettingsView } from "@/components/settings/settings-view";

export default function SettingsPage(): React.ReactElement {
  return (
    <AdminOnly>
      <SettingsView />
    </AdminOnly>
  );
}
