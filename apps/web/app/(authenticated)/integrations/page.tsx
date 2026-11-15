import { AdminOnly } from "@/components/common/admin-only";
import { IntegrationsView } from "@/components/integrations/integrations-view";

export default function IntegrationsPage(): React.ReactElement {
  return (
    <AdminOnly>
      <IntegrationsView />
    </AdminOnly>
  );
}
